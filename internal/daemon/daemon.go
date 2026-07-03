package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
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
)

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
	if sc.Mode != "http" {
		return nil, errors.New("remote shell daemon requires HTTP sync mode")
	}
	if sc.ServerURL == "" {
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
	for {
		if err := runOnce(ctx, rt); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func runOnce(ctx context.Context, rt *runtimeConfig) error {
	header := http.Header{}
	if rt.sync.APIKey != "" {
		header.Set("Authorization", "Bearer "+rt.sync.APIKey)
	}
	c, err := esync.DialWebSocket(ctx, esync.WSURLCandidates(rt.sync.ServerURL, "/api/v1/ws/daemon"), header, rt.sync.InsecureTLS)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	keepaliveCtx, stopKeepalive := context.WithCancel(ctx)
	defer stopKeepalive()
	wskeepalive.Start(keepaliveCtx, c, wsKeepaliveInterval, wsKeepaliveTimeout)

	hello, _ := json.Marshal(relay.HelloPayload{Role: "daemon", Tenant: rt.tenantID, PeerID: rt.peerID, Name: rt.name, Version: 1})
	if err := c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
		return err
	}

	var writeMu sync.Mutex
	var sessionsMu sync.Mutex
	sessions := map[uint32]*internalssh.InteractiveSession{}
	defer closeSessions(&sessionsMu, sessions)
	writeFrame := func(f relay.Frame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
		defer cancel()
		return c.Write(wctx, websocket.MessageBinary, relay.Encode(f))
	}

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
		handleFrame(rt, f, &sessionsMu, sessions, writeFrame, ctx)
	}
}

func handleFrame(rt *runtimeConfig, f relay.Frame, sessionsMu *sync.Mutex, sessions map[uint32]*internalssh.InteractiveSession, writeFrame func(relay.Frame) error, ctx context.Context) {
	switch f.Type {
	case relay.FrameOpen:
		if writeFrame == nil {
			return
		}
		go func() {
			reqCtx, cancel := context.WithTimeout(ctx, openRequestTimeout)
			defer cancel()
			handleOpen(rt, f, sessionsMu, sessions, writeFrame, reqCtx, ctx)
		}()
	case relay.FrameData:
		is := getSession(sessionsMu, sessions, f.StreamID)
		if is != nil && is.Stdin != nil {
			_, _ = is.Stdin.Write(f.Payload)
		}
	case relay.FrameResize:
		rows, cols, err := relay.ParseResize(f.Payload)
		if err == nil {
			is := getSession(sessionsMu, sessions, f.StreamID)
			if is != nil && is.Resize != nil {
				_ = is.Resize(rows, cols)
			}
		}
	case relay.FrameClose:
		if is := removeSession(sessionsMu, sessions, f.StreamID, nil); is != nil {
			_ = is.Close()
		}
	}
}

func closeSessions(mu *sync.Mutex, sessions map[uint32]*internalssh.InteractiveSession) {
	mu.Lock()
	open := make([]*internalssh.InteractiveSession, 0, len(sessions))
	for streamID, is := range sessions {
		open = append(open, is)
		delete(sessions, streamID)
	}
	mu.Unlock()
	for _, is := range open {
		_ = is.Close()
	}
}

func getSession(mu *sync.Mutex, sessions map[uint32]*internalssh.InteractiveSession, streamID uint32) *internalssh.InteractiveSession {
	mu.Lock()
	defer mu.Unlock()
	return sessions[streamID]
}

func setSession(mu *sync.Mutex, sessions map[uint32]*internalssh.InteractiveSession, streamID uint32, is *internalssh.InteractiveSession) {
	mu.Lock()
	sessions[streamID] = is
	mu.Unlock()
}

func removeSession(mu *sync.Mutex, sessions map[uint32]*internalssh.InteractiveSession, streamID uint32, expected *internalssh.InteractiveSession) *internalssh.InteractiveSession {
	mu.Lock()
	defer mu.Unlock()
	is := sessions[streamID]
	if is == nil {
		return nil
	}
	if expected != nil && is != expected {
		return nil
	}
	delete(sessions, streamID)
	return is
}

func handleOpen(rt *runtimeConfig, f relay.Frame, sessionsMu *sync.Mutex, sessions map[uint32]*internalssh.InteractiveSession, writeFrame func(relay.Frame) error, ctx context.Context, streamCtx context.Context) {
	var req relay.OpenRequest
	if err := json.Unmarshal(f.Payload, &req); err != nil {
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
		return
	}
	rows, cols := req.Rows, req.Cols
	rows, cols = internalssh.NormalizePTYSize(rows, cols)
	switch req.Target {
	case relay.TargetTmuxList:
		list, err := tmuxListSessions(ctx)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		payload, _ := json.Marshal(list)
		if writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: payload}) == nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
		}
	case relay.TargetTmuxNew:
		is, name, err := tmuxNewSession(ctx, rows, cols)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		setSession(sessionsMu, sessions, f.StreamID, is)
		if err := writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: []byte(name)}); err != nil {
			if removeSession(sessionsMu, sessions, f.StreamID, is) != nil {
				_ = is.Close()
			}
			return
		}
		go pumpSession(streamCtx, f.StreamID, is, writeFrame, func(streamID uint32, is *internalssh.InteractiveSession) bool {
			return removeSession(sessionsMu, sessions, streamID, is) != nil
		})
	case relay.TargetTmuxAttach:
		is, err := tmuxAttachSession(ctx, req.SessionID, rows, cols)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		setSession(sessionsMu, sessions, f.StreamID, is)
		if err := writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}); err != nil {
			if removeSession(sessionsMu, sessions, f.StreamID, is) != nil {
				_ = is.Close()
			}
			return
		}
		go pumpSession(streamCtx, f.StreamID, is, writeFrame, func(streamID uint32, is *internalssh.InteractiveSession) bool {
			return removeSession(sessionsMu, sessions, streamID, is) != nil
		})
	case relay.TargetTmuxKill:
		if err := tmuxKillSession(ctx, req.SessionID); err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		if writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}) == nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
		}
	case relay.TargetTmuxRename:
		if err := tmuxRenameSession(ctx, req.SessionID, req.Name); err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		if writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}) == nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameClose, StreamID: f.StreamID})
		}
	default:
		is, err := openTarget(rt, req, rows, cols)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		setSession(sessionsMu, sessions, f.StreamID, is)
		if err := writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID}); err != nil {
			if removeSession(sessionsMu, sessions, f.StreamID, is) != nil {
				_ = is.Close()
			}
			return
		}
		go pumpSession(streamCtx, f.StreamID, is, writeFrame, func(streamID uint32, is *internalssh.InteractiveSession) bool {
			return removeSession(sessionsMu, sessions, streamID, is) != nil
		})
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

func pumpSession(ctx context.Context, streamID uint32, is *internalssh.InteractiveSession, writeFrame func(relay.Frame) error, cleanup func(uint32, *internalssh.InteractiveSession) bool) {
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := is.Stdout.Read(buf)
			if n > 0 {
				payload := make([]byte, n)
				copy(payload, buf[:n])
				if err := writeFrame(relay.Frame{Type: relay.FrameData, StreamID: streamID, Payload: payload}); err != nil {
					done <- err
					return
				}
			}
			if err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
	case <-is.Done:
	case <-done:
	}
	if cleanup == nil || cleanup(streamID, is) {
		_ = is.Close()
		_ = writeFrame(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
	}
}
