package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

func init() {
	RegisterEngine(EngineDescriptor{
		ID:    "assemblyai",
		Label: "AssemblyAI",
		Params: []ParamSpec{
			{Key: "api_key", Label: "AssemblyAI API key", Secret: true, Required: true},
		},
		Ready: func(params map[string]string) bool { return params["api_key"] != "" },
		New: func(params map[string]string, feed FeedDeps) (Engine, error) {
			return newAssemblyAIFeed(AssemblyAIConfig{
				APIKey: params["api_key"],
			}, LocalConfig{
				VAD:                feed.VAD,
				OnDownloadProgress: feed.OnDownloadProgress,
			}), nil
		},
	})
}

const (
	defaultAssemblyAIURL   = "wss://api.assemblyai.com/v2/realtime/ws"
	assemblyAIFinalTimeout = 8 * time.Second
)

// AssemblyAIConfig configures the AssemblyAI realtime ASR engine.
type AssemblyAIConfig struct {
	APIKey string
	URL    string // default defaultAssemblyAIURL
}

func (c AssemblyAIConfig) wsURL() string {
	base := c.URL
	if base == "" {
		base = defaultAssemblyAIURL
	}
	return base + "?sample_rate=16000"
}

// AssemblyAIEngine is one AssemblyAI realtime stream: 16kHz mono S16LE PCM
// in via WriteAudio, partial transcripts out as partials, the final
// transcript emitted on Stop (Terminate).
type AssemblyAIEngine struct {
	cfg AssemblyAIConfig

	mu        sync.Mutex
	conn      *websocket.Conn
	started   bool
	closed    bool
	stopping  bool
	sentAudio bool
	finalText string // last FinalTranscript text
	interim   string // last PartialTranscript text

	events  chan Event
	done    chan struct{}
	wg      sync.WaitGroup
	finalCh chan struct{} // closed when the stream drains during Stop
}

func NewAssemblyAIEngine(cfg AssemblyAIConfig) *AssemblyAIEngine {
	return &AssemblyAIEngine{
		cfg:    cfg,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

func (e *AssemblyAIEngine) Events() <-chan Event { return e.events }

func (e *AssemblyAIEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}
	if e.cfg.APIKey == "" {
		return fmt.Errorf("assemblyai: APIKey required")
	}

	header := http.Header{}
	header.Set("Authorization", e.cfg.APIKey)
	conn, resp, err := websocket.Dial(ctx, e.cfg.wsURL(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("assemblyai auth failed: %s", resp.Status)
		}
		return fmt.Errorf("assemblyai dial: %w", err)
	}

	e.conn = conn
	e.started = true
	e.stopping = false
	e.sentAudio = false
	e.finalText = ""
	e.interim = ""
	e.finalCh = make(chan struct{})
	e.wg.Add(1)
	go e.readLoop(conn, e.finalCh)
	return nil
}

func (e *AssemblyAIEngine) WriteAudio(pcm []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.conn == nil {
		return fmt.Errorf("assemblyai: not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.conn.Write(ctx, websocket.MessageBinary, pcm); err != nil {
		return fmt.Errorf("assemblyai send audio: %w", err)
	}
	e.sentAudio = true
	return nil
}

// Stop sends Terminate and waits for the stream to drain; the final
// transcript surfaces as a final event from the read loop.
func (e *AssemblyAIEngine) Stop() error {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return nil
	}
	e.started = false
	e.stopping = true
	conn := e.conn
	e.conn = nil
	finalCh := e.finalCh
	sentAudio := e.sentAudio
	e.mu.Unlock()

	if conn == nil {
		return nil
	}
	if sentAudio {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Terminate"}`))
		cancel()
		select {
		case <-finalCh:
		case <-time.After(assemblyAIFinalTimeout):
		case <-e.done:
		}
	}
	conn.Close(websocket.StatusNormalClosure, "stop")
	return nil
}

func (e *AssemblyAIEngine) SetVAD(VADParams) error {
	// Endpointing is client-side (helper VAD); nothing to apply.
	return nil
}

func (e *AssemblyAIEngine) Close() error {
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

func (e *AssemblyAIEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

type assemblyAIMsg struct {
	MessageType string `json:"message_type"`
	Text        string `json:"text"`
}

func (e *AssemblyAIEngine) readLoop(conn *websocket.Conn, finalCh chan struct{}) {
	defer e.wg.Done()
	var finalOnce sync.Once
	defer e.finish(finalCh, &finalOnce)

	for {
		_, data, err := conn.Read(context.Background())
		if err != nil {
			e.mu.Lock()
			active := !e.closed && e.started
			e.mu.Unlock()
			if active {
				e.emit(Event{Type: EventError, Msg: fmt.Sprintf("assemblyai connection lost: %v", err)})
			}
			return
		}
		var msg assemblyAIMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Text == "" {
			continue
		}
		switch msg.MessageType {
		case "PartialTranscript":
			e.mu.Lock()
			e.interim = msg.Text
			e.mu.Unlock()
			e.emit(Event{Type: EventPartial, Text: msg.Text})
		case "FinalTranscript":
			e.mu.Lock()
			e.finalText = msg.Text
			e.mu.Unlock()
		}
	}
}

// finish emits the drained final transcript once the stream ends during Stop
// and releases the Stop wait.
func (e *AssemblyAIEngine) finish(finalCh chan struct{}, once *sync.Once) {
	once.Do(func() {
		e.mu.Lock()
		text := e.finalText
		if text == "" {
			text = e.interim
		}
		stopping := e.stopping
		e.mu.Unlock()
		if stopping && text != "" {
			e.emit(Event{Type: EventFinal, Text: text})
		}
		close(finalCh)
	})
}

func newAssemblyAIFeed(cfg AssemblyAIConfig, hcfg LocalConfig) *streamFeedEngine {
	return newStreamFeedEngine(func() streamSession { return NewAssemblyAIEngine(cfg) }, hcfg)
}
