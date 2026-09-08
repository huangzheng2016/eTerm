package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

func init() {
	RegisterEngine(EngineDescriptor{
		ID:    "deepgram",
		Label: "Deepgram",
		Params: []ParamSpec{
			{Key: "api_key", Label: "Deepgram API key", Secret: true, Required: true},
			{Key: "model", Label: "Model", Default: "nova-2-general"},
			{Key: "language", Label: "Language", Default: "zh"},
		},
		Ready: func(params map[string]string) bool { return params["api_key"] != "" },
		New: func(params map[string]string, feed FeedDeps) (Engine, error) {
			return newDeepgramFeed(DeepgramConfig{
				APIKey:   params["api_key"],
				Model:    params["model"],
				Language: params["language"],
			}, LocalConfig{
				VAD:                feed.VAD,
				OnDownloadProgress: feed.OnDownloadProgress,
			}), nil
		},
	})
}

const (
	defaultDeepgramURL   = "wss://api.deepgram.com/v1/listen"
	deepgramFinalTimeout = 8 * time.Second
)

type DeepgramConfig struct {
	APIKey   string
	Model    string
	Language string
	URL      string
}

func (c DeepgramConfig) wsURL() string {
	base := c.URL
	if base == "" {
		base = defaultDeepgramURL
	}
	model := c.Model
	if model == "" {
		model = "nova-2-general"
	}
	lang := c.Language
	if lang == "" {
		lang = "zh"
	}
	q := url.Values{
		"encoding":        {"linear16"},
		"sample_rate":     {"16000"},
		"channels":        {"1"},
		"interim_results": {"true"},
		"model":           {model},
		"language":        {lang},
	}
	return base + "?" + q.Encode()
}

type DeepgramEngine struct {
	cfg DeepgramConfig

	mu        sync.Mutex
	conn      *websocket.Conn
	started   bool
	closed    bool
	stopping  bool
	sentAudio bool
	finals    []string
	interim   string

	events  chan Event
	done    chan struct{}
	wg      sync.WaitGroup
	finalCh chan struct{}
}

func NewDeepgramEngine(cfg DeepgramConfig) *DeepgramEngine {
	return &DeepgramEngine{
		cfg:    cfg,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

func (e *DeepgramEngine) Events() <-chan Event { return e.events }

func (e *DeepgramEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}
	if e.cfg.APIKey == "" {
		return fmt.Errorf("deepgram: APIKey required")
	}

	header := http.Header{}
	header.Set("Authorization", "Token "+e.cfg.APIKey)
	conn, resp, err := websocket.Dial(ctx, e.cfg.wsURL(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return fmt.Errorf("deepgram auth failed: %s", resp.Status)
		}
		return fmt.Errorf("deepgram dial: %w", err)
	}

	e.conn = conn
	e.started = true
	e.stopping = false
	e.sentAudio = false
	e.finals = nil
	e.interim = ""
	e.finalCh = make(chan struct{})
	e.wg.Add(1)
	go e.readLoop(conn, e.finalCh)
	return nil
}

func (e *DeepgramEngine) WriteAudio(pcm []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started || e.conn == nil {
		return fmt.Errorf("deepgram: not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.conn.Write(ctx, websocket.MessageBinary, pcm); err != nil {
		return fmt.Errorf("deepgram send audio: %w", err)
	}
	e.sentAudio = true
	return nil
}

func (e *DeepgramEngine) Stop() error {
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
		conn.Write(ctx, websocket.MessageText, []byte(`{"type":"CloseStream"}`))
		cancel()
		select {
		case <-finalCh:
		case <-time.After(deepgramFinalTimeout):
		case <-e.done:
		}
	}
	conn.Close(websocket.StatusNormalClosure, "stop")
	return nil
}

func (e *DeepgramEngine) SetVAD(VADParams) error {
	return nil
}

func (e *DeepgramEngine) Close() error {
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

func (e *DeepgramEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

type deepgramResult struct {
	Type    string `json:"type"`
	IsFinal bool   `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
		} `json:"alternatives"`
	} `json:"channel"`
}

func (e *DeepgramEngine) readLoop(conn *websocket.Conn, finalCh chan struct{}) {
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
				e.emit(Event{Type: EventError, Msg: fmt.Sprintf("deepgram connection lost: %v", err)})
			}
			return
		}
		var res deepgramResult
		if err := json.Unmarshal(data, &res); err != nil {
			continue
		}
		if res.Type != "Results" || len(res.Channel.Alternatives) == 0 {
			continue
		}
		text := res.Channel.Alternatives[0].Transcript
		if text == "" {
			continue
		}
		e.mu.Lock()
		if res.IsFinal {
			e.finals = append(e.finals, text)
		} else {
			e.interim = text
		}
		e.mu.Unlock()
		if !res.IsFinal {
			e.emit(Event{Type: EventPartial, Text: text})
		}
	}
}

func (e *DeepgramEngine) finish(finalCh chan struct{}, once *sync.Once) {
	once.Do(func() {
		e.mu.Lock()
		text := strings.Join(e.finals, " ")
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

type streamSession interface {
	Start(ctx context.Context) error
	WriteAudio(pcm []byte) error
	Stop() error
	Close() error
	Events() <-chan Event
}

type streamFeedEngine struct {
	dial func() streamSession
	hcfg LocalConfig

	mu      sync.Mutex
	helper  *LocalEngine
	sess    streamSession
	started bool
	closed  bool
	idleCh  chan struct{}
	pumped  bool

	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup
}

func newStreamFeedEngine(dial func() streamSession, hcfg LocalConfig) *streamFeedEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &streamFeedEngine{
		dial:   dial,
		hcfg:   hcfg,
		ctx:    ctx,
		cancel: cancel,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

func newDeepgramFeed(cfg DeepgramConfig, hcfg LocalConfig) *streamFeedEngine {
	return newStreamFeedEngine(func() streamSession { return NewDeepgramEngine(cfg) }, hcfg)
}

func (e *streamFeedEngine) Events() <-chan Event { return e.events }

func (e *streamFeedEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}

	sess := e.dial()
	if err := sess.Start(ctx); err != nil {
		return err
	}

	if e.helper == nil {
		hcfg := e.hcfg
		hcfg.Passthrough = true
		hcfg.OnAudio = e.onAudio
		hcfg.OnUtteranceEnd = e.onUtteranceEnd
		e.helper = NewLocalEngine(hcfg)
	}

	e.sess = sess
	e.idleCh = make(chan struct{}, 1)
	e.started = true
	if err := e.helper.Start(ctx); err != nil {
		e.started = false
		e.sess = nil
		sess.Close()
		return err
	}

	if !e.pumped {
		e.pumped = true
		e.wg.Add(1)
		go e.pumpHelper(e.helper.Events())
	}
	e.wg.Add(1)
	go e.pump(sess.Events())
	return nil
}

func (e *streamFeedEngine) Stop() error {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return nil
	}
	e.started = false
	helper := e.helper
	idleCh := e.idleCh
	e.mu.Unlock()

	if helper != nil {
		_ = helper.Stop()
		select {
		case <-idleCh:
		default:
		}
		select {
		case <-idleCh:
		case <-time.After(time.Second):
		case <-e.done:
		}
	}

	e.mu.Lock()
	sess := e.sess
	e.sess = nil
	e.mu.Unlock()
	if sess != nil {
		sess.Stop()
		sess.Close()
	}
	return nil
}

func (e *streamFeedEngine) SetVAD(p VADParams) error {
	e.mu.Lock()
	e.hcfg.VAD = p
	helper := e.helper
	e.mu.Unlock()
	if helper == nil {
		return nil
	}
	return helper.SetVAD(p)
}

func (e *streamFeedEngine) SetModel(string, string) error { return nil }

func (e *streamFeedEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.started = false
	helper := e.helper
	sess := e.sess
	e.sess = nil
	e.mu.Unlock()

	close(e.done)
	e.cancel()
	if helper != nil {
		helper.Close()
	}
	if sess != nil {
		sess.Close()
	}
	e.wg.Wait()
	close(e.events)
	return nil
}

func (e *streamFeedEngine) onAudio(pcm []byte) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	sess := e.sess
	e.mu.Unlock()
	if sess == nil {
		return
	}
	_ = sess.WriteAudio(pcm)
}

func (e *streamFeedEngine) onUtteranceEnd() {
	e.mu.Lock()
	if !e.started || e.closed {
		e.mu.Unlock()
		return
	}
	old := e.sess
	e.mu.Unlock()

	type redial struct {
		sess streamSession
		err  error
	}
	dialed := make(chan redial, 1)
	go func() {
		sess := e.dial()
		ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
		err := sess.Start(ctx)
		cancel()
		dialed <- redial{sess, err}
	}()

	if old != nil {
		old.Stop()
		old.Close()
	}

	res := <-dialed
	e.mu.Lock()
	if e.sess == old {
		e.sess = nil
	}
	if !e.started || e.closed {
		e.mu.Unlock()
		res.sess.Close()
		return
	}
	if res.err != nil {
		e.mu.Unlock()
		e.emit(Event{Type: EventError, Msg: fmt.Sprintf("reconnect: %v", res.err)})
		return
	}
	e.sess = res.sess
	e.wg.Add(1)
	e.mu.Unlock()

	go e.pump(res.sess.Events())
}

func (e *streamFeedEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

func (e *streamFeedEngine) pump(ch <-chan Event) {
	defer e.wg.Done()
	for ev := range ch {
		e.emit(ev)
	}
}

func (e *streamFeedEngine) pumpHelper(ch <-chan Event) {
	defer e.wg.Done()
	for ev := range ch {
		if ev.Type == EventState && ev.State == StateIdle {
			e.signalIdle()
		}
		e.emit(ev)
	}
}

func (e *streamFeedEngine) signalIdle() {
	e.mu.Lock()
	ch := e.idleCh
	e.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}
