package voice

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func init() {
	RegisterEngine(EngineDescriptor{
		ID:    "volcano",
		Label: "Volcano Engine",
		Params: []ParamSpec{
			{Key: "api_key", Label: "Volcano API key", Secret: true, Required: true},
			{Key: "resource_id", Label: "Volcano model", Default: ResourceIDSeedASR, Options: VolcanoResourceIDs},
		},
		Ready: func(params map[string]string) bool {
			return params["api_key"] != ""
		},
		New: func(params map[string]string, feed FeedDeps) (Engine, error) {
			return NewVolcanoFeedEngine(VolcanoFeedConfig{
				Volcano: VolcanoConfig{
					APIKey:      params["api_key"],
					ResourceID:  params["resource_id"],
					SmartFormat: true,
				},
				Helper: LocalConfig{
					VAD:                feed.VAD,
					OnDownloadProgress: feed.OnDownloadProgress,
				},
			}), nil
		},
	})
}

type VolcanoFeedConfig struct {
	Volcano VolcanoConfig
	Helper  LocalConfig
}

type VolcanoFeedEngine struct {
	vcfg VolcanoConfig
	hcfg LocalConfig

	mu        sync.Mutex
	helper    *LocalEngine
	vol       *VolcanoEngine
	buf       []byte
	started   bool
	closed    bool
	idleCh    chan struct{}
	pumped    bool
	contextFn func() string

	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewVolcanoFeedEngine(cfg VolcanoFeedConfig) *VolcanoFeedEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &VolcanoFeedEngine{
		vcfg:   cfg.Volcano,
		hcfg:   cfg.Helper,
		ctx:    ctx,
		cancel: cancel,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

func (e *VolcanoFeedEngine) Events() <-chan Event { return e.events }

func (e *VolcanoFeedEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}

	vol := NewVolcanoEngine(e.dialConfigLocked())
	if err := vol.Start(ctx); err != nil {
		return err
	}

	if e.helper == nil {
		hcfg := e.hcfg
		hcfg.Passthrough = true
		hcfg.OnAudio = e.onAudio
		hcfg.OnUtteranceEnd = e.onUtteranceEnd
		e.helper = NewLocalEngine(hcfg)
	}

	e.vol = vol
	e.idleCh = make(chan struct{}, 1)
	e.started = true
	if err := e.helper.Start(ctx); err != nil {
		e.started = false
		e.vol = nil
		vol.Close()
		return err
	}

	if !e.pumped {
		e.pumped = true
		e.wg.Add(1)
		go e.pumpHelper(e.helper.Events())
	}
	e.wg.Add(1)
	go e.pump(vol.Events())
	return nil
}

func (e *VolcanoFeedEngine) Stop() error {
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

	e.flushAudio()

	e.mu.Lock()
	vol := e.vol
	e.vol = nil
	e.mu.Unlock()
	if vol != nil {
		vol.Stop()
		vol.Close()
	}
	return nil
}

func (e *VolcanoFeedEngine) SetVAD(p VADParams) error {
	e.mu.Lock()
	e.hcfg.VAD = p
	helper := e.helper
	e.mu.Unlock()
	if helper == nil {
		return nil
	}
	return helper.SetVAD(p)
}

func (e *VolcanoFeedEngine) SetModel(string, string) error { return nil }

func (e *VolcanoFeedEngine) SetContext(ctx string) error {
	e.mu.Lock()
	e.vcfg.Context = ctx
	vol := e.vol
	e.mu.Unlock()
	if vol != nil {
		return vol.SetContext(ctx)
	}
	return nil
}

// SetContextProvider installs a func that returns fresh corpus.context before
// every dial (Start and each per-utterance redial). A nil func restores the
// static SetContext value. A panicking func is treated as empty context.
func (e *VolcanoFeedEngine) SetContextProvider(fn func() string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contextFn = fn
}

// dialConfigLocked returns vcfg with corpus.context refreshed from the
// provider. Callers must hold e.mu.
func (e *VolcanoFeedEngine) dialConfigLocked() VolcanoConfig {
	e.vcfg.Context = safeContext(e.contextFn, e.vcfg.Context)
	return e.vcfg
}

// safeContext calls fn and returns its value; a nil or panicking fn yields
// the fallback static context.
func safeContext(fn func() string, fallback string) (s string) {
	if fn == nil {
		return fallback
	}
	defer func() {
		if recover() != nil {
			s = ""
		}
	}()
	return fn()
}

func (e *VolcanoFeedEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.started = false
	helper := e.helper
	vol := e.vol
	e.vol = nil
	e.mu.Unlock()

	close(e.done)
	e.cancel()
	if helper != nil {
		helper.Close()
	}
	if vol != nil {
		vol.Close()
	}
	e.wg.Wait()
	close(e.events)
	return nil
}

// volcanoPCMFlushBytes is 200ms of 16kHz 16bit mono PCM, the recommended
// per-frame payload for the volcano streaming API.
const volcanoPCMFlushBytes = 6400

func (e *VolcanoFeedEngine) onAudio(pcm []byte) {
	e.mu.Lock()
	if e.closed || e.vol == nil {
		e.mu.Unlock()
		return
	}
	e.buf = append(e.buf, pcm...)
	if len(e.buf) < volcanoPCMFlushBytes {
		e.mu.Unlock()
		return
	}
	chunk := e.buf
	e.buf = nil
	vol := e.vol
	e.mu.Unlock()
	_ = vol.WriteAudio(chunk)
}

func (e *VolcanoFeedEngine) flushAudio() {
	e.mu.Lock()
	if len(e.buf) == 0 {
		e.mu.Unlock()
		return
	}
	chunk := e.buf
	e.buf = nil
	vol := e.vol
	e.mu.Unlock()
	if vol != nil {
		_ = vol.WriteAudio(chunk)
	}
}

func (e *VolcanoFeedEngine) onUtteranceEnd() {
	e.mu.Lock()
	if !e.started || e.closed {
		e.mu.Unlock()
		return
	}
	old := e.vol
	e.mu.Unlock()

	type redial struct {
		vol *VolcanoEngine
		err error
	}
	dialed := make(chan redial, 1)
	go func() {
		e.mu.Lock()
		cfg := e.dialConfigLocked()
		e.mu.Unlock()
		vol := NewVolcanoEngine(cfg)
		ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
		err := vol.Start(ctx)
		cancel()
		dialed <- redial{vol, err}
	}()

	if old != nil {
		old.Stop()
		old.Close()
	}

	res := <-dialed
	e.mu.Lock()
	if e.vol == old {
		e.vol = nil
	}
	if !e.started || e.closed {
		e.mu.Unlock()
		res.vol.Close()
		return
	}
	if res.err != nil {
		e.mu.Unlock()
		e.emit(Event{Type: EventError, Msg: fmt.Sprintf("volcano reconnect: %v", res.err)})
		return
	}
	e.vol = res.vol
	e.wg.Add(1)
	e.mu.Unlock()

	go e.pump(res.vol.Events())
}

func (e *VolcanoFeedEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

func (e *VolcanoFeedEngine) pump(ch <-chan Event) {
	defer e.wg.Done()
	for ev := range ch {
		e.emit(ev)
	}
}

func (e *VolcanoFeedEngine) pumpHelper(ch <-chan Event) {
	defer e.wg.Done()
	for ev := range ch {
		if ev.Type == EventState && ev.State == StateIdle {
			e.signalIdle()
		}
		e.emit(ev)
	}
}

func (e *VolcanoFeedEngine) signalIdle() {
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
