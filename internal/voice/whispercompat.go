package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterEngine(EngineDescriptor{
		ID:    "whispercompat",
		Label: "Whisper-compatible API",
		Params: []ParamSpec{
			{Key: "base_url", Label: "Base URL", Default: defaultWhisperCompatBaseURL},
			{Key: "api_key", Label: "API key", Secret: true},
			{Key: "model", Label: "Model", Default: "whisper-1"},
		},
		Ready: func(params map[string]string) bool {
			if params["api_key"] != "" {
				return true
			}
			return whisperCompatLocalhost(params["base_url"])
		},
		New: func(params map[string]string, feed FeedDeps) (Engine, error) {
			return NewWhisperCompatFeedEngine(WhisperCompatConfig{
				BaseURL: params["base_url"],
				APIKey:  params["api_key"],
				Model:   params["model"],
			}, LocalConfig{
				VAD:                feed.VAD,
				OnDownloadProgress: feed.OnDownloadProgress,
			}), nil
		},
	})
}

const (
	defaultWhisperCompatBaseURL = "https://api.openai.com/v1"
	whisperCompatFinalTimeout   = 30 * time.Second
)

// whisperCompatHTTPClient bounds one transcription POST; a hung endpoint
// must not stall the single transcription queue until Close.
var whisperCompatHTTPClient = &http.Client{Timeout: whisperCompatFinalTimeout}

// whisperCompatLocalhost reports whether base points at a local server, in
// which case no API key is required. Empty means the default (remote) URL.
func whisperCompatLocalhost(base string) bool {
	if base == "" {
		return false
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// WhisperCompatConfig configures an OpenAI-compatible transcription endpoint
// (OpenAI /v1, Groq, SiliconFlow, or a custom server).
type WhisperCompatConfig struct {
	BaseURL string // default defaultWhisperCompatBaseURL
	APIKey  string // optional for localhost endpoints
	Model   string // default whisper-1
}

// WhisperCompatFeedEngine buffers helper passthrough PCM per utterance and
// posts it to {base_url}/audio/transcriptions as a WAV multipart form; the
// JSON text field becomes the final transcript.
type WhisperCompatFeedEngine struct {
	cfg  WhisperCompatConfig
	hcfg LocalConfig

	mu      sync.Mutex
	helper  *LocalEngine
	buf     []byte // current utterance PCM
	started bool
	closed  bool
	idleCh  chan struct{}
	pumped  bool

	pending sync.WaitGroup // in-flight transcriptions, drained by Stop
	queue   chan []byte

	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewWhisperCompatFeedEngine(cfg WhisperCompatConfig, hcfg LocalConfig) *WhisperCompatFeedEngine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &WhisperCompatFeedEngine{
		cfg:    cfg,
		hcfg:   hcfg,
		queue:  make(chan []byte, 8),
		ctx:    ctx,
		cancel: cancel,
		events: make(chan Event, 256),
		done:   make(chan struct{}),
	}
	e.wg.Add(1)
	go e.work()
	return e
}

func (e *WhisperCompatFeedEngine) Events() <-chan Event { return e.events }

func (e *WhisperCompatFeedEngine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("voice: engine closed")
	}
	if e.started {
		return nil
	}

	if e.helper == nil {
		hcfg := e.hcfg
		hcfg.Passthrough = true
		hcfg.OnAudio = e.onAudio
		hcfg.OnUtteranceEnd = e.onUtteranceEnd
		e.helper = NewLocalEngine(hcfg)
	}

	e.buf = nil
	e.idleCh = make(chan struct{}, 1)
	e.started = true
	if err := e.helper.Start(ctx); err != nil {
		e.started = false
		return err
	}

	if !e.pumped {
		// the helper persists across sessions, so pump it once
		e.pumped = true
		e.wg.Add(1)
		go e.pumpHelper(e.helper.Events())
	}
	return nil
}

// Stop ends the session: the helper flushes its tail audio, the buffered
// utterance is queued, and in-flight transcriptions drain.
func (e *WhisperCompatFeedEngine) Stop() error {
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
		// wait for the stop flush (tail audio + state idle)
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

	e.flush()

	drained := make(chan struct{})
	go func() {
		e.pending.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(whisperCompatFinalTimeout):
	case <-e.done:
	}
	return nil
}

// flush queues any buffered tail audio for transcription.
func (e *WhisperCompatFeedEngine) flush() {
	e.mu.Lock()
	pcm := e.buf
	e.buf = nil
	if len(pcm) > 0 {
		e.pending.Add(1)
	}
	e.mu.Unlock()
	if len(pcm) > 0 {
		e.queue <- pcm
	}
}

func (e *WhisperCompatFeedEngine) SetVAD(p VADParams) error {
	e.mu.Lock()
	e.hcfg.VAD = p
	helper := e.helper
	e.mu.Unlock()
	if helper == nil {
		return nil
	}
	return helper.SetVAD(p)
}

// SetModel is a no-op: the passthrough helper does no local ASR.
func (e *WhisperCompatFeedEngine) SetModel(string, string) error { return nil }

func (e *WhisperCompatFeedEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.started = false
	helper := e.helper
	e.mu.Unlock()

	close(e.done)
	e.cancel() // abort in-flight transcriptions
	if helper != nil {
		helper.Close()
	}
	e.wg.Wait()
	close(e.events)
	return nil
}

// onAudio appends one helper PCM chunk to the current utterance buffer. Runs
// on the helper read loop, serialized with onUtteranceEnd.
func (e *WhisperCompatFeedEngine) onAudio(pcm []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.buf = append(e.buf, pcm...)
}

// onUtteranceEnd queues the buffered utterance for transcription.
func (e *WhisperCompatFeedEngine) onUtteranceEnd() {
	e.mu.Lock()
	if !e.started || e.closed {
		e.mu.Unlock()
		return
	}
	pcm := e.buf
	e.buf = nil
	if len(pcm) > 0 {
		e.pending.Add(1)
	}
	e.mu.Unlock()
	if len(pcm) > 0 {
		e.queue <- pcm
	}
}

func (e *WhisperCompatFeedEngine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.done:
	}
}

// work transcribes queued utterances one at a time, keeping finals ordered.
func (e *WhisperCompatFeedEngine) work() {
	defer e.wg.Done()
	for {
		select {
		case pcm := <-e.queue:
			text, err := whisperCompatTranscribe(e.ctx, e.cfg, pcm)
			if err != nil {
				e.emit(Event{Type: EventError, Msg: err.Error()})
			} else if text != "" {
				e.emit(Event{Type: EventFinal, Text: text})
			}
			e.pending.Done()
		case <-e.done:
			return
		}
	}
}

// pumpHelper forwards helper events and signals the idle that ends a stop
// flush (and any mid-session cancel).
func (e *WhisperCompatFeedEngine) pumpHelper(ch <-chan Event) {
	defer e.wg.Done()
	for ev := range ch {
		if ev.Type == EventState && ev.State == StateIdle {
			e.signalIdle()
		}
		e.emit(ev)
	}
}

func (e *WhisperCompatFeedEngine) signalIdle() {
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

// whisperCompatTranscribe posts one utterance of 16kHz mono S16LE PCM as a
// WAV file to an OpenAI-compatible transcription endpoint.
func whisperCompatTranscribe(ctx context.Context, cfg WhisperCompatConfig, pcm []byte) (string, error) {
	base := cfg.BaseURL
	if base == "" {
		base = defaultWhisperCompatBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = "whisper-1"
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", model); err != nil {
		return "", err
	}
	fw, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="audio.wav"`},
		"Content-Type":        {"audio/wav"},
	})
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(wavWrap(pcm)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(base, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := whisperCompatHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper transcribe: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("whisper transcribe: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(data))
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return "", fmt.Errorf("whisper transcribe: %s: %s", resp.Status, snippet)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("whisper transcribe: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

// wavWrap wraps 16kHz mono S16LE PCM in a RIFF/WAVE header.
func wavWrap(pcm []byte) []byte {
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+len(pcm)))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)    // fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:], 1)     // PCM format
	binary.LittleEndian.PutUint16(buf[22:], 1)     // mono
	binary.LittleEndian.PutUint32(buf[24:], 16000) // sample rate
	binary.LittleEndian.PutUint32(buf[28:], 32000) // byte rate
	binary.LittleEndian.PutUint16(buf[32:], 2)     // block align
	binary.LittleEndian.PutUint16(buf[34:], 16)    // bits per sample
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(len(pcm)))
	copy(buf[44:], pcm)
	return buf
}
