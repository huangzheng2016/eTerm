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
	shells   *shellManager
}

type wsOpen struct {
	PeerID     string `json:"peer_id"`
	Target     string `json:"target"`
	HostSyncID string `json:"host_sync_id,omitempty"`
	ShellID    string `json:"shell_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Cols       int    `json:"cols,omitempty"`
}

type wsHello struct {
	Role    string `json:"role"`
	Tenant  string `json:"tenant"`
	PeerID  string `json:"peer_id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

const (
	wsKeepaliveInterval = 25 * time.Second
	wsKeepaliveTimeout  = 5 * time.Second
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
	rt := &runtimeConfig{
		db:       database,
		mk:       mk,
		sync:     sc,
		name:     name,
		peerID:   peerID,
		tenantID: sc.TenantID(),
	}
	rt.shells = newShellManager(
		func(rows, cols int) (*internalssh.InteractiveSession, error) {
			configured, _ := db.GetSetting(database, localterm.SettingShell)
			return localterm.NewSession(localterm.DefaultShell(configured), rows, cols)
		},
		func() string {
			configured, _ := db.GetSetting(database, localterm.SettingShell)
			return localterm.DefaultShell(configured)
		},
	)
	return rt, nil
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
	c, err := dialDaemon(ctx, esync.WSURLCandidates(rt.sync.ServerURL, "/api/v1/ws/daemon"), header, rt.sync.InsecureTLS)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	keepaliveCtx, stopKeepalive := context.WithCancel(ctx)
	defer stopKeepalive()
	wskeepalive.Start(keepaliveCtx, c, wsKeepaliveInterval, wsKeepaliveTimeout)

	hello, _ := json.Marshal(wsHello{Role: "daemon", Tenant: rt.tenantID, PeerID: rt.peerID, Name: rt.name, Version: 1})
	if err := c.Write(ctx, websocket.MessageBinary, relay.Encode(relay.Frame{Type: relay.FrameHello, Payload: hello})); err != nil {
		return err
	}

	var mu sync.Mutex
	sessions := map[uint32]*internalssh.InteractiveSession{}
	activeStreams := map[uint32]string{}
	rt.shells.detachAll()
	writeFrame := func(f relay.Frame) error {
		mu.Lock()
		defer mu.Unlock()
		return c.Write(ctx, websocket.MessageBinary, relay.Encode(f))
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
		switch f.Type {
		case relay.FrameOpen:
			handleOpen(rt, f, sessions, activeStreams, writeFrame, ctx)
		case relay.FrameData:
			if shellID, ok := activeStreams[f.StreamID]; ok {
				if s := rt.shells.get(shellID); s != nil && s.is.Stdin != nil {
					_, _ = s.is.Stdin.Write(f.Payload)
				}
			} else if is := sessions[f.StreamID]; is != nil && is.Stdin != nil {
				_, _ = is.Stdin.Write(f.Payload)
			}
		case relay.FrameResize:
			rows, cols, err := relay.ParseResize(f.Payload)
			if err == nil {
				if shellID, ok := activeStreams[f.StreamID]; ok {
					if s := rt.shells.get(shellID); s != nil && s.is.Resize != nil {
						_ = s.is.Resize(rows, cols)
					}
				} else if is := sessions[f.StreamID]; is != nil && is.Resize != nil {
					_ = is.Resize(rows, cols)
				}
			}
		case relay.FrameClose:
			if _, ok := activeStreams[f.StreamID]; ok {
				rt.shells.detachStream(f.StreamID)
				delete(activeStreams, f.StreamID)
			} else if is := sessions[f.StreamID]; is != nil {
				_ = is.Close()
				delete(sessions, f.StreamID)
			}
		}
	}
}

func handleOpen(rt *runtimeConfig, f relay.Frame, sessions map[uint32]*internalssh.InteractiveSession, activeStreams map[uint32]string, writeFrame func(relay.Frame) error, ctx context.Context) {
	var req wsOpen
	if err := json.Unmarshal(f.Payload, &req); err != nil {
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
		return
	}
	rows, cols := req.Rows, req.Cols
	if cols < 40 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}
	switch req.Target {
	case relay.TargetActiveList:
		payload, _ := json.Marshal(rt.shells.list())
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: payload})
	case relay.TargetActiveNew:
		s, err := rt.shells.create(f.StreamID, rows, cols, writeFrame)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		activeStreams[f.StreamID] = s.id
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID, Payload: []byte(s.id)})
		s.start()
	case relay.TargetActiveAttach:
		s, displaced, replay, err := rt.shells.attach(req.ShellID, f.StreamID, rows, cols, writeFrame)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		if displaced != 0 {
			delete(activeStreams, displaced)
		}
		activeStreams[f.StreamID] = s.id
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		_ = writeReplay(f.StreamID, replay, writeFrame)
	case relay.TargetActiveKill:
		rt.shells.kill(req.ShellID)
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
	case relay.TargetActiveRename:
		if err := rt.shells.rename(req.ShellID, req.Name); err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
	default:
		is, err := openTarget(rt, f.Payload)
		if err != nil {
			_ = writeFrame(relay.Frame{Type: relay.FrameOpenErr, StreamID: f.StreamID, Payload: []byte(err.Error())})
			return
		}
		sessions[f.StreamID] = is
		_ = writeFrame(relay.Frame{Type: relay.FrameOpenOK, StreamID: f.StreamID})
		go pumpSession(ctx, f.StreamID, is, writeFrame)
	}
}

const activeReplayChunkSize = 16 * 1024

func writeReplay(streamID uint32, replay []byte, writeFrame func(relay.Frame) error) error {
	for len(replay) > 0 {
		n := activeReplayChunkSize
		if len(replay) < n {
			n = len(replay)
		}
		if err := writeFrame(relay.Frame{Type: relay.FrameData, StreamID: streamID, Payload: replay[:n]}); err != nil {
			return err
		}
		replay = replay[n:]
	}
	return nil
}

func dialDaemon(ctx context.Context, urls []string, header http.Header, insecureTLS bool) (*websocket.Conn, error) {
	var lastErr error
	client := esync.HTTPClient(30*time.Second, insecureTLS)
	for _, u := range urls {
		conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: header, HTTPClient: client})
		if err != nil {
			lastErr = err
			continue
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("server URL is required")
}

func openTarget(rt *runtimeConfig, payload []byte) (*internalssh.InteractiveSession, error) {
	var req wsOpen
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	rows, cols := req.Rows, req.Cols
	if cols < 40 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}
	switch req.Target {
	case "local":
		configured, _ := db.GetSetting(rt.db, localterm.SettingShell)
		return localterm.NewSession(localterm.DefaultShell(configured), rows, cols)
	case "host":
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

func pumpSession(ctx context.Context, streamID uint32, is *internalssh.InteractiveSession, writeFrame func(relay.Frame) error) {
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
	_ = is.Close()
	_ = writeFrame(relay.Frame{Type: relay.FrameClose, StreamID: streamID})
}
