package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/huangzheng2016/eTerm/internal/config"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/localterm"
	"github.com/huangzheng2016/eTerm/internal/relay"
	"github.com/huangzheng2016/eTerm/internal/security"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	esync "github.com/huangzheng2016/eTerm/internal/sync"
	"github.com/huangzheng2016/eTerm/internal/tmux"
	"github.com/huangzheng2016/eTerm/internal/wskeepalive"
	"gorm.io/gorm"
)

type Config struct {
	DBPath   string
	Password string
	Name     string
}

type runtimeConfig struct {
	db       *gorm.DB
	mk       *security.MasterKeyManager
	sync     esync.Config
	name     string
	peerID   string
	tenantID string
}

const (
	wsKeepaliveInterval = 25 * time.Second
	wsKeepaliveTimeout  = 5 * time.Second
	wsWriteTimeout      = 10 * time.Second
	openRequestTimeout  = 30 * time.Second
	sessionStartupGrace = 150 * time.Millisecond
	maxOutputFrameBytes = 16 * 1024
)

var errProtocolVersion = errors.New("relay protocol version mismatch")

var (
	tmuxListSessions  = tmux.ListSessions
	tmuxNewSession    = tmux.NewSession
	tmuxAttachSession = tmux.AttachSession
	tmuxKillSession   = tmux.KillSession
	tmuxRenameSession = tmux.RenameSession
)

func Run(ctx context.Context, cfg Config) error {
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = config.DBPath()
	}
	database, err := db.InitDB(dbPath)
	if err != nil {
		return err
	}
	rt, err := loadRuntime(database, cfg)
	if err != nil {
		return err
	}
	log.Printf("eterm daemon starting name=%q peer=%s tenant=%s server=%s", rt.name, rt.peerID, shortID(rt.tenantID), rt.sync.ServerURL)
	return runLoop(ctx, rt)
}

func loadRuntime(database *gorm.DB, cfg Config) (*runtimeConfig, error) {
	mk, err := unlock(database, cfg.Password)
	if err != nil {
		return nil, err
	}
	sc := esync.LoadConfig(database, mk)
	if !sc.Enabled {
		return nil, errors.New("sync is disabled")
	}
	if sc.Mode == "ssh" {
		if sc.SSHHostID == 0 {
			return nil, errors.New("no SSH host configured for sync")
		}
	} else if sc.ServerURL == "" {
		return nil, errors.New("sync server URL is required")
	}
	if sc.Passphrase == "" {
		return nil, errors.New("sync passphrase is required")
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = "eterm"
	}
	peerID, _ := db.GetSetting(database, "remote_peer_id")
	if peerID == "" {
		peerID = uuid.New().String()
		_ = db.SetSetting(database, "remote_peer_id", peerID)
	}
	return &runtimeConfig{
		db:       database,
		mk:       mk,
		sync:     sc,
		name:     name,
		peerID:   peerID,
		tenantID: sc.TenantID(),
	}, nil
}

func unlock(database *gorm.DB, password string) (*security.MasterKeyManager, error) {
	saltStr, err := db.GetSetting(database, "encryption_salt")
	if err != nil {
		return nil, errors.New("database encryption is not initialized")
	}
	salt, err := base64.StdEncoding.DecodeString(saltStr)
	if err != nil {
		return nil, err
	}
	verifierStr, err := db.GetSetting(database, "encryption_verifier")
	if err != nil {
		return nil, err
	}
	verifier, err := base64.StdEncoding.DecodeString(verifierStr)
	if err != nil {
		return nil, err
	}
	mk := security.NewMasterKeyManager(salt, verifier, 24*time.Hour)
	noPassword, _ := db.GetSetting(database, "no_password")
	if noPassword == "true" {
		mk.UnlockNoPassword()
		return mk, nil
	}
	if password == "" {
		password = os.Getenv("ETERM_MASTER_PASSWORD")
	}
	if password == "" || !mk.Unlock([]byte(password)) {
		return nil, errors.New("invalid or missing master password")
	}
	return mk, nil
}

func runLoop(ctx context.Context, rt *runtimeConfig) error {
	mgr := newSessionManager()
	defer mgr.closeAll()
	go mgr.reapLoop(ctx)
	delay := 2 * time.Second
	for {
		start := time.Now()
		if err := runOnce(ctx, rt, mgr); err != nil {
			if errors.Is(err, errProtocolVersion) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("eterm daemon relay disconnected: %v", err)
		}
		if time.Since(start) > 30*time.Second {
			delay = 2 * time.Second // connection lived; not a connect failure
		}
		log.Printf("eterm daemon relay reconnecting in %s", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		if delay < 60*time.Second {
			delay *= 2
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
		}
	}
}

func runOnce(ctx context.Context, rt *runtimeConfig, mgr *sessionManager) error {
	serverURL := rt.sync.ServerURL
	insecureTLS := rt.sync.InsecureTLS
	if rt.sync.Mode == "ssh" {
		tunnel, err := esync.OpenTunnel(rt.db, rt.mk, rt.sync.SSHHostID, rt.sync.RemotePort)
		if err != nil {
			return err
		}
		defer tunnel.Close()
		serverURL = tunnel.BaseURL()
		insecureTLS = false
	}
	header := http.Header{}
	if rt.sync.APIKey != "" {
		header.Set("Authorization", "Bearer "+rt.sync.APIKey)
	}
	c, err := esync.DialWebSocket(ctx, esync.WSURLCandidates(serverURL, "/api/v1/ws/daemon"), header, insecureTLS)
	if err != nil {
		return err
	}
	log.Printf("eterm daemon relay connected")
	defer c.CloseNow()
	keepaliveCtx, stopKeepalive := context.WithCancel(ctx)
	defer stopKeepalive()
	wskeepalive.Start(keepaliveCtx, c, wsKeepaliveInterval, wsKeepaliveTimeout)

	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: rt.tenantID, PeerID: rt.peerID, Name: rt.name, Version: relay.ProtocolVersion})
	if err := c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
		return err
	}
	log.Printf("eterm daemon relay registered peer=%s tenant=%s", rt.peerID, shortID(rt.tenantID))

	connCtx, cancelConn := context.WithCancel(ctx)
	defer cancelConn()
	sender := newFrameSender()
	mgr.setSender(sender)
	defer mgr.clearSender(sender)
	go sender.run(connCtx, c)

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageBinary {
			continue
		}
		f, err := relay.Decode(data)
		if err != nil {
			continue
		}
		if f.Type == relay.FrameHelloErr {
			return fmt.Errorf("%w: %s", errProtocolVersion, f.Payload)
		}
		handleFrame(rt, f, mgr, sender, ctx)
	}
}

func shortID(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func handleFrame(rt *runtimeConfig, f relay.Frame, mgr *sessionManager, sender *frameSender, ctx context.Context) {
	switch f.Type {
	case relay.FrameOpen:
		if sender == nil {
			return
		}
		go func() {
			reqCtx, cancel := context.WithTimeout(ctx, openRequestTimeout)
			defer cancel()
			handleOpen(rt, f, mgr, sender, reqCtx, ctx)
		}()
	case relay.FrameData:
		if sr := mgr.get(f.StreamID); sr != nil && sr.is.Stdin != nil {
			_, _ = sr.is.Stdin.Write(f.Payload)
		}
	case relay.FrameResize:
		rows, cols, err := relay.ParseResize(f.Payload)
		if err == nil {
			if sr := mgr.get(f.StreamID); sr != nil && sr.is.Resize != nil {
				_ = sr.is.Resize(rows, cols)
			}
		}
	case relay.FrameAck:
		ack, err := relay.ParseAck(f.Payload)
		if err == nil {
			if sr := mgr.get(f.StreamID); sr != nil {
				sr.setAck(ack)
			}
		}
	case relay.FrameClose:
		if string(f.Payload) == relay.CloseClientDisconnected {
			// The client connection dropped; keep the PTY for a later resume.
			if sr := mgr.get(f.StreamID); sr != nil {
				sr.markDetached()
			}
			return
		}
		if sr := mgr.remove(f.StreamID, nil); sr != nil {
			sr.shutdown()
			_ = sr.is.Close()
		}
	}
}

const resumeUnavailableErr = "resume unavailable"

func handleOpen(rt *runtimeConfig, f relay.Frame, mgr *sessionManager, sender *frameSender, ctx context.Context, streamCtx context.Context) {
	var req relay.OpenRequest
	if err := json.Unmarshal(f.Payload, &req); err != nil {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
		return
	}
	if sr := mgr.get(f.StreamID); sr != nil {
		// Reconnect on an existing stream: replay retained output from the
		// client's last consumed offset. OpenOK must reach the client before
		// any replayed data, so attachForOpen holds the stream lock while
		// queueing it.
		openOK := relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}
		if err := sr.attachForOpen(req.ResumeFromSeq, sender, openOK); err != nil {
			_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(resumeUnavailableErr)})
		}
		return
	}
	if req.ResumeFromSeq > 0 {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(resumeUnavailableErr)})
		return
	}
	rows, cols := req.Rows, req.Cols
	rows, cols = internalssh.NormalizePTYSize(rows, cols)
	configFile := ""
	if req.Target == relay.TargetTmuxList || req.Target == relay.TargetTmuxNew || req.Target == relay.TargetTmuxAttach || req.Target == relay.TargetTmuxKill || req.Target == relay.TargetTmuxRename {
		home, err := os.UserHomeDir()
		if err != nil {
			_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		configFile, err = tmux.ResolveConfig(rt.db, config.ConfigDir(), home)
		if err != nil {
			_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
	}
	openErr := func(err error) {
		_ = sender.send(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
	}
	// startStream registers the session, replies OpenOK, then starts the pump.
	startStream := func(is *internalssh.InteractiveSession, okPayload []byte) {
		sr := newStreamRelay(is)
		mgr.add(f.StreamID, sr)
		if err := sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: okPayload}); err != nil {
			if mgr.remove(f.StreamID, sr) != nil {
				sr.shutdown()
				_ = is.Close()
			}
			return
		}
		go sr.pump(streamCtx, f.StreamID, mgr)
	}
	switch req.Target {
	case relay.TargetTmuxList:
		list, err := tmuxListSessions(ctx, configFile)
		if err != nil {
			openErr(err)
			return
		}
		payload, _ := json.Marshal(list)
		if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: payload}) == nil {
			_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
		}
	case relay.TargetTmuxNew:
		is, name, err := tmuxNewSession(ctx, configFile, rows, cols)
		if err != nil {
			openErr(err)
			return
		}
		if err := waitSessionStarted(is); err != nil {
			_ = is.Close()
			_ = tmuxKillSession(ctx, configFile, name)
			openErr(err)
			return
		}
		startStream(is, []byte(name))
	case relay.TargetTmuxAttach:
		is, err := tmuxAttachSession(ctx, configFile, req.SessionID, rows, cols)
		if err != nil {
			openErr(err)
			return
		}
		if err := waitSessionStarted(is); err != nil {
			_ = is.Close()
			openErr(err)
			return
		}
		startStream(is, nil)
	case relay.TargetTmuxKill:
		if err := tmuxKillSession(ctx, configFile, req.SessionID); err != nil {
			openErr(err)
			return
		}
		if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}) == nil {
			_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
		}
	case relay.TargetTmuxRename:
		if err := tmuxRenameSession(ctx, configFile, req.SessionID, req.Name); err != nil {
			openErr(err)
			return
		}
		if sender.send(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}) == nil {
			_ = sender.send(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
		}
	default:
		is, err := openTarget(rt, req, rows, cols)
		if err != nil {
			openErr(err)
			return
		}
		if err := waitSessionStarted(is); err != nil {
			_ = is.Close()
			openErr(err)
			return
		}
		startStream(is, nil)
	}
}

func waitSessionStarted(is *internalssh.InteractiveSession) error {
	select {
	case err := <-is.Done:
		if err != nil {
			return err
		}
		return errors.New("terminal exited immediately")
	case <-time.After(sessionStartupGrace):
		return nil
	}
}

func openTarget(rt *runtimeConfig, req relay.OpenRequest, rows, cols int) (*internalssh.InteractiveSession, error) {
	switch req.Target {
	case relay.TargetLocal:
		configured, _ := db.GetSetting(rt.db, localterm.SettingShell)
		return localterm.NewSession(localterm.DefaultShell(configured), rows, cols)
	case relay.TargetHost:
		return openHost(rt, req.HostSyncID, rows, cols)
	default:
		return nil, fmt.Errorf("unknown target %q", req.Target)
	}
}

func openHost(rt *runtimeConfig, syncID string, rows, cols int) (*internalssh.InteractiveSession, error) {
	var host db.Host
	if err := rt.db.Preload("Key").Where("sync_id = ?", syncID).First(&host).Error; err != nil {
		return nil, err
	}
	var hostKey *db.SSHKey
	if host.KeyID != nil {
		hostKey = &host.Key
	}
	var jumpHost *db.Host
	var jumpKey *db.SSHKey
	if host.JumpHostID != nil {
		var jh db.Host
		if rt.db.Preload("Key").First(&jh, *host.JumpHostID).Error == nil {
			jumpHost = &jh
			if jh.KeyID != nil {
				jumpKey = &jh.Key
			}
		}
	}
	res, err := internalssh.Connect(internalssh.ConnectConfig{
		Host:      &host,
		Key:       hostKey,
		JumpHost:  jumpHost,
		JumpKey:   jumpKey,
		MasterKey: rt.mk,
		DB:        rt.db,
		FingerprintCallback: func(string, int, string, string) bool {
			return true
		},
	})
	if err != nil {
		return nil, err
	}
	is, err := internalssh.NewInteractiveSession(res.Client, rows, cols, host.ForwardAgent)
	if err != nil {
		res.Close()
		return nil, err
	}
	is.SetClosers(res.Closers)
	return is, nil
}

func sessionDoneErr(readErr error, sessionDone <-chan error) error {
	select {
	case err := <-sessionDone:
		return err
	case <-time.After(250 * time.Millisecond):
		return readErr
	}
}

func closePayload(err error) []byte {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) {
		return nil
	}
	return []byte(err.Error())
}
