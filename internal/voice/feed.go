package voice

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// VolcanoFeedConfig configures the volcano feed engine: a passthrough helper
// (capture+VAD) streaming PCM into Volcano cloud ASR sessions.
type VolcanoFeedConfig struct {
	Volcano VolcanoConfig
	Helper  LocalConfig // Passthrough/OnAudio/OnUtteranceEnd are managed internally
}

// VolcanoFeedEngine routes helper passthrough audio to VolcanoEngine.WriteAudio.
// The helper VAD marks utterance ends; each utterance gets its own volcano
// session (negative-seq final frame, final transcript collected), then a
// fresh connection is dialed for the next utterance.
type VolcanoFeedEngine struct {
	vcfg VolcanoConfig
	hcfg LocalConfig

	mu      sync.Mutex
	helper  *LocalEngine
	vol     *VolcanoEngine // current utterance session; nil while redialing
	started bool
	closed  bool
	idleCh  chan struct{} // helper state-idle signal, ends the stop flush
	pumped  bool          // helper pump goroutine is running

	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewVolcanoFeedEngine(cfg VolcanoFeedConfig) *VolcanoFeedEngine {
	return &VolcanoFeedEngine{
		vcfg:   cfg.Volcano,
		hcfg:   cfg.Helper,
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

	vol := NewVolcanoEngine(e.vcfg)
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
	if err := e.helper.Start(ctx); err != nil {
		vol.Close()
		return err
	}

	e.vol = vol
	e.idleCh = make(chan struct{}, 1)
	e.started = true
	if !e.pumped {
		// the helper persists across sessions, so pump it once
		e.pumped = true
		e.wg.Add(1)
		go e.pumpHelper(e.helper.Events())
	}
	e.wg.Add(1)
	go e.pump(vol.Events())
	return nil
}

// Stop ends the session: the helper flushes its tail audio, then the current
// volcano session is finalized (negative-seq frame) and closed.
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
		// wait for the stop flush (tail audio + state idle) before finalizing
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

// onAudio streams one helper PCM chunk into the current volcano session.
// Runs on the helper read loop, serialized with onUtteranceEnd.
func (e *VolcanoFeedEngine) onAudio(pcm []byte) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	vol := e.vol
	e.mu.Unlock()
	if vol == nil {
		return
	}
	// a dead session is reported by its own read loop
	_ = vol.WriteAudio(pcm)
}

// onUtteranceEnd finalizes the current volcano session and dials the next.
func (e *VolcanoFeedEngine) onUtteranceEnd() {
	e.mu.Lock()
	if !e.started || e.closed {
		e.mu.Unlock()
		return
	}
	old := e.vol
	e.vol = nil
	e.mu.Unlock()

	if old != nil {
		old.Stop() // negative-seq final; transcript drains through its pump
		old.Close()
	}

	vol := NewVolcanoEngine(e.vcfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := vol.Start(ctx)
	cancel()

	e.mu.Lock()
	if !e.started || e.closed {
		e.mu.Unlock()
		vol.Close()
		return
	}
	if err != nil {
		e.mu.Unlock()
		e.emit(Event{Type: EventError, Msg: fmt.Sprintf("volcano reconnect: %v", err)})
		return
	}
	e.vol = vol
	e.wg.Add(1)
	e.mu.Unlock()

	go e.pump(vol.Events())
}

func (e *VolcanoFeedEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

// pump forwards one volcano session's events until its channel closes.
func (e *VolcanoFeedEngine) pump(ch <-chan Event) {
	defer e.wg.Done()
	for ev := range ch {
		e.emit(ev)
	}
}

// pumpHelper forwards helper events and signals the idle that ends a stop
// flush (and any mid-session cancel).
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
