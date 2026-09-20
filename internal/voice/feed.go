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
					APIKey:        params["api_key"],
					ResourceID:    params["resource_id"],
					SmartFormat:   true,
					DDC:           feed.DDC,
					EndWindowSize: feed.EndWindowSize,
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
	staticCtx string
	lastGood  string

	redialing   bool
	redialAgain bool
	gen         uint64

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
	e.gen++
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
	e.staticCtx = ctx
	vol := e.vol
	e.mu.Unlock()
	if vol != nil {
		return vol.SetContext(ctx)
	}
	return nil
}

// SetContextProvider installs a func that returns fresh corpus.context before
// every dial (Start and each per-utterance redial). A non-empty result is
// cached as the last-good context; a panicking func falls back to the
// last-good value (empty when none). A nil func restores the static
// SetContext value.
func (e *VolcanoFeedEngine) SetContextProvider(fn func() string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contextFn = fn
}

// dialConfigLocked returns vcfg with corpus.context refreshed from the
// provider. Callers must hold e.mu.
func (e *VolcanoFeedEngine) dialConfigLocked() VolcanoConfig {
	e.vcfg.Context = e.contextFromProvider()
	return e.vcfg
}

// contextFromProvider resolves corpus.context for the next dial. A non-empty
// provider result becomes the last-good value; a panicking provider reuses
// the last-good value (empty when none). Callers must hold e.mu.
func (e *VolcanoFeedEngine) contextFromProvider() string {
	if e.contextFn == nil {
		return e.staticCtx
	}
	s, ok := callContext(e.contextFn)
	if !ok {
		return e.lastGood
	}
	if s != "" {
		e.lastGood = s
	}
	return s
}

// callContext calls fn, reporting failure on panic.
func callContext(fn func() string) (s string, ok bool) {
	defer func() {
		if recover() != nil {
			s, ok = "", false
		}
	}()
	return fn(), true
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

// pcmFlushBytes is 200ms of 16kHz 16bit mono PCM, the recommended per-frame
// payload for streaming speech APIs.
const pcmFlushBytes = 6400

func (e *VolcanoFeedEngine) onAudio(pcm []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || !e.started {
		return
	}
	e.buf = append(e.buf, pcm...)
	if e.vol == nil || len(e.buf) < pcmFlushBytes {
		return
	}
	chunk := e.buf
	e.buf = nil
	_ = e.vol.WriteAudio(chunk)
}

func (e *VolcanoFeedEngine) flushAudio() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.buf) == 0 || e.vol == nil {
		return
	}
	chunk := e.buf
	e.buf = nil
	_ = e.vol.WriteAudio(chunk)
}

// onUtteranceEnd starts an asynchronous redial so the helper read loop is
// never blocked by the dial. The old connection is stopped (after flushing
// the buffered audio tail to it) while the new connection is dialed in
// parallel; audio arriving during the redial window stays buffered and is
// replayed to the new connection once it is ready.
func (e *VolcanoFeedEngine) onUtteranceEnd() {
	e.mu.Lock()
	if !e.started || e.closed {
		e.mu.Unlock()
		return
	}
	if e.redialing {
		e.redialAgain = true
		e.mu.Unlock()
		return
	}
	e.redialing = true
	old := e.vol
	e.vol = nil
	gen := e.gen
	var tail []byte
	if old != nil {
		tail = e.buf
		e.buf = nil
	}
	e.wg.Add(1)
	e.mu.Unlock()
	go e.redial(old, tail, gen)
}

func (e *VolcanoFeedEngine) redial(old *VolcanoEngine, tail []byte, gen uint64) {
	defer e.wg.Done()
	type result struct {
		vol *VolcanoEngine
		err error
	}
	dialed := make(chan result, 1)
	go func() {
		e.mu.Lock()
		cfg := e.dialConfigLocked()
		e.mu.Unlock()
		vol := NewVolcanoEngine(cfg)
		ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
		err := vol.Start(ctx)
		cancel()
		dialed <- result{vol, err}
	}()

	if old != nil {
		if len(tail) > 0 {
			_ = old.WriteAudio(tail)
		}
		old.Stop()
		old.Close()
	}

	res := <-dialed
	e.mu.Lock()
	if !e.started || e.closed || gen != e.gen {
		e.redialing = false
		again := e.redialAgain
		e.redialAgain = false
		e.mu.Unlock()
		res.vol.Close()
		if again {
			e.onUtteranceEnd()
		}
		return
	}
	if res.err != nil {
		e.redialing = false
		again := e.redialAgain
		e.redialAgain = false
		e.mu.Unlock()
		e.emit(Event{Type: EventError, Msg: fmt.Sprintf("volcano reconnect: %v", res.err)})
		if again {
			e.onUtteranceEnd()
		}
		return
	}
	e.vol = res.vol
	replay := e.buf
	e.buf = nil
	again := e.redialAgain
	e.redialAgain = false
	e.redialing = false
	e.wg.Add(1)
	for off := 0; off < len(replay); off += pcmFlushBytes {
		_ = res.vol.WriteAudio(replay[off:min(off+pcmFlushBytes, len(replay))])
	}
	e.mu.Unlock()

	go e.pump(res.vol.Events())
	if again {
		e.onUtteranceEnd()
	}
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
