package voice

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	minHelperProtocol = 1
	maxHelperRestarts = 3
	handshakeTimeout  = 10 * time.Second
)

// LocalConfig configures the local (voicehelper subprocess) engine.
type LocalConfig struct {
	BinPath     string // explicit helper binary path; skips lookup/download
	ModelDir    string // passed as -model-dir; empty uses helper default
	CacheDir    string // helper download cache; default os.UserCacheDir()/eterm
	DownloadURL string
	SHA256Hex   string // expected sha256 of the downloaded binary; empty skips verify
	VAD         VADParams

	OnDownloadProgress func(pct float64)
}

type helperCommand struct {
	Cmd  string `json:"cmd"`
	Path string `json:"path,omitempty"`

	Threshold       *float64 `json:"threshold,omitempty"`
	MinSilence      *float64 `json:"min_silence,omitempty"`
	MinSpeech       *float64 `json:"min_speech,omitempty"`
	TrailingSilence *float64 `json:"trailing_silence,omitempty"`
	MaxSegment      *float64 `json:"max_segment,omitempty"`
	NoSpeechTimeout *float64 `json:"no_speech_timeout,omitempty"`
}

type helperEvent struct {
	Type     string  `json:"type"`
	Version  string  `json:"version"`
	Protocol int     `json:"protocol"`
	Text     string  `json:"text"`
	State    string  `json:"state"`
	Msg      string  `json:"msg"`
	Pct      float64 `json:"pct"`
}

// LocalEngine drives the voicehelper subprocess over NDJSON stdin/stdout.
type LocalEngine struct {
	cfg LocalConfig

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	out       *bufio.Reader
	started   bool
	closed    bool
	restarts  int
	spawnedAt time.Time
	vad       VADParams

	wg     sync.WaitGroup
	events chan Event
	done   chan struct{}
}

func NewLocalEngine(cfg LocalConfig) *LocalEngine {
	return &LocalEngine{
		cfg:    cfg,
		vad:    cfg.VAD,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
}

func (e *LocalEngine) Events() <-chan Event { return e.events }

func (e *LocalEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}
	if e.cmd == nil {
		if err := e.spawnLocked(ctx); err != nil {
			return err
		}
	}
	if err := e.sendVADLocked(); err != nil {
		return err
	}
	if err := e.sendLocked(helperCommand{Cmd: "start"}); err != nil {
		return err
	}
	e.started = true
	return nil
}

func (e *LocalEngine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started {
		return nil
	}
	e.started = false
	e.restarts = 0
	if e.cmd == nil {
		// helper already gone (crashed); nothing to stop
		return nil
	}
	return e.sendLocked(helperCommand{Cmd: "stop"})
}

func (e *LocalEngine) SetVAD(p VADParams) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vad = p
	if e.cmd == nil {
		return nil
	}
	return e.sendVADLocked()
}

func (e *LocalEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.started = false
	cmd := e.cmd
	if cmd != nil {
		cmd.Process.Kill()
	}
	e.mu.Unlock()

	close(e.done)
	e.wg.Wait()
	close(e.events)
	return nil
}

// spawnLocked starts the helper process and validates the handshake.
func (e *LocalEngine) spawnLocked(ctx context.Context) error {
	bin, err := ensureHelperBinary(ctx, e.cfg)
	if err != nil {
		return err
	}

	args := []string{}
	if e.cfg.ModelDir != "" {
		args = append(args, "-model-dir", e.cfg.ModelDir)
	}
	cmd := exec.Command(bin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start voice helper: %w", err)
	}

	e.cmd = cmd
	e.stdin = stdin
	e.out = bufio.NewReader(stdout)
	e.spawnedAt = time.Now()

	if err := e.handshakeLocked(); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		e.cmd = nil
		return err
	}

	e.wg.Add(1)
	go e.readLoop(cmd, e.out)
	return nil
}

func (e *LocalEngine) handshakeLocked() error {
	type result struct {
		ev  helperEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := e.out.ReadBytes('\n')
		if err != nil {
			ch <- result{err: err}
			return
		}
		var ev helperEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{ev: ev}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("voice helper handshake: %w", r.err)
		}
		if r.ev.Type != "hello" {
			return fmt.Errorf("voice helper handshake: expected hello, got %q", r.ev.Type)
		}
		if r.ev.Protocol < minHelperProtocol {
			return fmt.Errorf("voice helper protocol %d too old (need >= %d)", r.ev.Protocol, minHelperProtocol)
		}
		return nil
	case <-time.After(handshakeTimeout):
		return fmt.Errorf("voice helper handshake: timeout")
	}
}

func (e *LocalEngine) sendVADLocked() error {
	p := e.vad
	cmd := helperCommand{Cmd: "set_vad_params"}
	set := func(dst **float64, v float64) {
		if v != 0 {
			*dst = &v
		}
	}
	set(&cmd.Threshold, p.Threshold)
	set(&cmd.MinSilence, p.MinSilence)
	set(&cmd.MinSpeech, p.MinSpeech)
	set(&cmd.TrailingSilence, p.TrailingSilence)
	set(&cmd.MaxSegment, p.MaxSegment)
	set(&cmd.NoSpeechTimeout, p.NoSpeechTimeout)
	return e.sendLocked(cmd)
}

func (e *LocalEngine) sendLocked(c helperCommand) error {
	if e.stdin == nil {
		return fmt.Errorf("voice helper not running")
	}
	line, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := e.stdin.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("voice helper send: %w", err)
	}
	return nil
}

func (e *LocalEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

// readLoop forwards helper events until the process exits, then restarts it
// if the session was active and the restart budget remains. All sends to
// e.events happen here (or in the restart chain), so Close can wait on e.wg
// before closing the channel.
func (e *LocalEngine) readLoop(cmd *exec.Cmd, out *bufio.Reader) {
	defer e.wg.Done()
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var hev helperEvent
		if err := json.Unmarshal(sc.Bytes(), &hev); err != nil {
			continue
		}
		e.emit(Event{Type: hev.Type, Text: hev.Text, State: hev.State, Msg: hev.Msg, Pct: hev.Pct})
	}
	cmd.Wait()

	e.mu.Lock()
	if e.closed || e.cmd != cmd {
		e.mu.Unlock()
		return
	}
	e.cmd = nil
	e.stdin = nil
	// a helper that survived a while earns a fresh restart budget
	if time.Since(e.spawnedAt) > time.Minute {
		e.restarts = 0
	}
	if !e.started || e.restarts >= maxHelperRestarts {
		restarts := e.restarts
		e.started = false
		e.mu.Unlock()
		if restarts >= maxHelperRestarts {
			e.emit(Event{Type: EventError, Msg: "voice helper crashed repeatedly; giving up"})
		} else {
			e.emit(Event{Type: EventState, State: StateIdle})
		}
		return
	}
	e.restarts++
	e.started = false // let Start respawn and re-enter listening
	e.mu.Unlock()

	time.Sleep(500 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.Start(ctx); err != nil {
		e.emit(Event{Type: EventError, Msg: fmt.Sprintf("voice helper restart: %v", err)})
		return
	}
	e.emit(Event{Type: EventError, Msg: "voice helper restarted after crash"})
}
