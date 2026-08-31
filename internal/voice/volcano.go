package voice

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultVolcanoURL     = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"
	ResourceIDSeedASR     = "volc.seedasr.sauc.duration"
	ResourceIDBigASR      = "volc.bigasr.sauc.duration"
	volcanoInitialTimeout = 5 * time.Second
	volcanoFinalTimeout   = 8 * time.Second
)

// VolcanoConfig configures the Volcano Engine cloud ASR engine. Auth uses
// either APIKey (X-Api-Key) or AppKey+AccessKey (X-Api-App-Key /
// X-Api-Access-Key).
type VolcanoConfig struct {
	APIKey      string
	AppKey      string
	AccessKey   string
	ResourceID  string // default ResourceIDSeedASR
	URL         string // default defaultVolcanoURL
	Language    string // default zh-CN
	SampleRate  int    // default 16000
	SmartFormat bool   // enable_itn + enable_punc
}

// VolcanoEngine is an in-process Volcano Engine realtime ASR client. Audio
// is fed by the caller via WriteAudio as 16kHz mono S16LE PCM.
type VolcanoEngine struct {
	cfg VolcanoConfig

	mu        sync.Mutex
	conn      *websocket.Conn
	seq       int32
	started   bool
	closed    bool
	sentAudio bool

	events  chan Event
	done    chan struct{}
	wg      sync.WaitGroup
	finalCh chan struct{} // closed when a final/error/close arrives during Stop
}

func NewVolcanoEngine(cfg VolcanoConfig) *VolcanoEngine {
	return &VolcanoEngine{
		cfg:    cfg,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

func (e *VolcanoEngine) Events() <-chan Event { return e.events }

func (e *VolcanoEngine) resourceID() string {
	if e.cfg.ResourceID != "" {
		return e.cfg.ResourceID
	}
	return ResourceIDSeedASR
}

func (e *VolcanoEngine) url() string {
	if e.cfg.URL != "" {
		return e.cfg.URL
	}
	return defaultVolcanoURL
}

func (e *VolcanoEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}
	if e.cfg.APIKey == "" && (e.cfg.AppKey == "" || e.cfg.AccessKey == "") {
		return fmt.Errorf("volcano: APIKey or AppKey+AccessKey required")
	}

	header := http.Header{}
	header.Set("X-Api-Resource-Id", e.resourceID())
	header.Set("X-Api-Connect-Id", fmt.Sprintf("eterm-%d", time.Now().UnixMilli()))
	if e.cfg.APIKey != "" {
		header.Set("X-Api-Key", e.cfg.APIKey)
	} else {
		header.Set("X-Api-App-Key", e.cfg.AppKey)
		header.Set("X-Api-Access-Key", e.cfg.AccessKey)
	}

	conn, resp, err := websocket.Dial(ctx, e.url(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("volcano auth failed: %s", resp.Status)
		}
		return fmt.Errorf("volcano dial: %w", err)
	}

	e.seq = 1
	frame, err := buildFullClientRequest(e.cfg, e.seq)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "build request")
		return err
	}
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		conn.Close(websocket.StatusInternalError, "write")
		return fmt.Errorf("volcano send config: %w", err)
	}

	if err := waitInitialResponse(ctx, conn); err != nil {
		conn.Close(websocket.StatusInternalError, "init")
		return err
	}

	e.conn = conn
	e.sentAudio = false
	e.started = true
	e.finalCh = make(chan struct{})
	e.wg.Add(1)
	go e.readLoop(conn, e.finalCh)
	return nil
}

func waitInitialResponse(ctx context.Context, conn *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, volcanoInitialTimeout)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("volcano initial response: %w", err)
	}
	if typ != websocket.MessageBinary {
		return fmt.Errorf("volcano initial response: unexpected message type %d", typ)
	}
	ev, err := parseServerFrame(data)
	if err != nil {
		return err
	}
	if ev != nil && ev.isError {
		return fmt.Errorf("volcano: %s", ev.msg)
	}
	return nil
}

func (e *VolcanoEngine) WriteAudio(pcm []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.conn == nil {
		return fmt.Errorf("volcano: not started")
	}
	e.seq++
	frame, err := buildAudioFrame(pcm, e.seq)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		return fmt.Errorf("volcano send audio: %w", err)
	}
	e.sentAudio = true
	return nil
}

func (e *VolcanoEngine) Stop() error {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return nil
	}
	e.started = false
	conn := e.conn
	e.conn = nil
	finalCh := e.finalCh
	sentAudio := e.sentAudio
	e.seq++
	seq := e.seq
	e.mu.Unlock()

	if conn == nil {
		return nil
	}

	if sentAudio {
		frame, err := buildAudioFrame(nil, -abs32(seq))
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			conn.Write(ctx, websocket.MessageBinary, frame)
			cancel()
		}
		select {
		case <-finalCh:
		case <-time.After(volcanoFinalTimeout):
		case <-e.done:
		}
	}
	conn.Close(websocket.StatusNormalClosure, "stop")
	return nil
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (e *VolcanoEngine) SetVAD(p VADParams) error {
	// Endpointing is server-side for Volcano; nothing to apply.
	return nil
}

func (e *VolcanoEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	conn := e.conn
	e.conn = nil
	e.mu.Unlock()

	close(e.done)
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "close")
	}
	e.wg.Wait()
	close(e.events)
	return nil
}

func (e *VolcanoEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

func (e *VolcanoEngine) readLoop(conn *websocket.Conn, finalCh chan struct{}) {
	defer e.wg.Done()
	var finalOnce sync.Once
	notifyFinal := func() { finalOnce.Do(func() { close(finalCh) }) }
	defer notifyFinal()

	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		ev, err := parseServerFrame(data)
		if err != nil {
			e.emit(Event{Type: EventError, Msg: err.Error()})
			continue
		}
		if ev == nil {
			continue
		}
		if ev.isError {
			e.emit(Event{Type: EventError, Msg: ev.msg})
			notifyFinal()
			continue
		}
		if ev.final {
			e.emit(Event{Type: EventFinal, Text: ev.text})
			notifyFinal()
		} else {
			e.emit(Event{Type: EventPartial, Text: ev.text})
		}
	}
}
