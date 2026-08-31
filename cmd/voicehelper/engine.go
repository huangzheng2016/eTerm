package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const sampleRate = 16000

type vadParams struct {
	threshold       float64
	minSilence      float64
	minSpeech       float64
	trailingSilence float64
	maxSegment      float64
	noSpeechTimeout float64
}

func defaultVADParams() vadParams {
	return vadParams{
		threshold:       0.5,
		minSilence:      0.3,
		minSpeech:       0.2,
		trailingSilence: 1.0,
		maxSegment:      30,
		noSpeechTimeout: 5,
	}
}

type capture struct {
	mu     sync.Mutex
	ctx    *malgo.AllocatedContext
	dev    *malgo.Device
	chunks chan []float32
}

func newCapture() *capture {
	return &capture{chunks: make(chan []float32, 256)}
}

func (c *capture) start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dev != nil {
		return nil
	}

	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = sampleRate
	cfg.PeriodSizeInMilliseconds = 20

	chunks := c.chunks
	dev, err := malgo.InitDevice(mctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, input []byte, framecount uint32) {
			n := int(framecount)
			samples := make([]float32, n)
			for i := 0; i < n && 2*i+1 < len(input); i++ {
				v := int16(binary.LittleEndian.Uint16(input[2*i:]))
				samples[i] = float32(v) / 32768.0
			}
			select {
			case chunks <- samples:
			default:
			}
		},
	})
	if err != nil {
		mctx.Free()
		return err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		mctx.Free()
		return err
	}
	c.ctx = mctx
	c.dev = dev
	return nil
}

func (c *capture) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dev == nil {
		return
	}
	c.dev.Stop()
	c.dev.Uninit()
	c.ctx.Free()
	c.dev = nil
	c.ctx = nil
}

// pcmBytes converts float32 samples back to 16kHz mono S16LE for streaming.
func pcmBytes(samples []float32) []byte {
	b := make([]byte, 2*len(samples))
	for i, s := range samples {
		v := int16(s * 32767)
		binary.LittleEndian.PutUint16(b[2*i:], uint16(v))
	}
	return b
}

type asrEngine struct {
	ev     *eventWriter
	params vadParams

	modelRoot string
	asrDir    string
	asrKind   string // recognizer family; empty = sensevoice
	vadPath   string

	rec *sherpa.OfflineRecognizer
	vad *sherpa.VoiceActivityDetector

	cap *capture

	passthrough bool // capture+VAD only: stream PCM, no local ASR

	state       string
	accumText   string
	accumSpeech time.Duration
	speechSeen  bool
	lastSpeech  time.Time
	listenSince time.Time
}

func newASREngine(ev *eventWriter, modelRoot string) *asrEngine {
	return &asrEngine{
		ev:        ev,
		params:    defaultVADParams(),
		modelRoot: modelRoot,
		cap:       newCapture(),
		state:     "idle",
	}
}

func (e *asrEngine) loadRecognizer() error {
	if e.asrDir == "" {
		return nil
	}
	kind := e.asrKind
	if kind == "" {
		kind = "sensevoice"
	}
	model, tokens, err := asrModelPaths(e.asrDir, kind)
	if err != nil {
		return err
	}
	cfg := &sherpa.OfflineRecognizerConfig{}
	cfg.FeatConfig.SampleRate = sampleRate
	cfg.FeatConfig.FeatureDim = 80
	cfg.ModelConfig.Tokens = tokens
	cfg.ModelConfig.NumThreads = 2
	cfg.ModelConfig.Provider = "cpu"
	switch kind {
	case "sensevoice", "sensevoice-int8":
		cfg.ModelConfig.SenseVoice.Model = model
		cfg.ModelConfig.SenseVoice.Language = "auto"
		cfg.ModelConfig.SenseVoice.UseInverseTextNormalization = 1
	case "paraformer":
		cfg.ModelConfig.Paraformer.Model = model
	default:
		return fmt.Errorf("unknown model kind: %s", kind)
	}
	rec := sherpa.NewOfflineRecognizer(cfg)
	if rec == nil {
		return fmt.Errorf("failed to load ASR model: %s", model)
	}
	if e.rec != nil {
		sherpa.DeleteOfflineRecognizer(e.rec)
	}
	e.rec = rec
	return nil
}

func (e *asrEngine) loadVAD() error {
	cfg := &sherpa.VadModelConfig{}
	cfg.SileroVad.Model = e.vadPath
	cfg.SileroVad.Threshold = float32(e.params.threshold)
	cfg.SileroVad.MinSilenceDuration = float32(e.params.minSilence)
	cfg.SileroVad.MinSpeechDuration = float32(e.params.minSpeech)
	cfg.SileroVad.MaxSpeechDuration = float32(e.params.maxSegment)
	cfg.SileroVad.WindowSize = 512
	cfg.SampleRate = sampleRate
	cfg.NumThreads = 1
	cfg.Provider = "cpu"
	vad := sherpa.NewVoiceActivityDetector(cfg, 60)
	if vad == nil {
		return fmt.Errorf("failed to load VAD model: %s", e.vadPath)
	}
	if e.vad != nil {
		sherpa.DeleteVoiceActivityDetector(e.vad)
	}
	e.vad = vad
	return nil
}

func (e *asrEngine) setState(s string) {
	if e.state != s {
		e.state = s
		e.ev.state(s)
	}
}

// start begins a listening session, downloading models on first use.
func (e *asrEngine) start(ctx context.Context) { e.startMode(ctx, false) }

// startPassthrough begins a capture+VAD session without local ASR: raw PCM
// chunks stream out as audio events and VAD finalizes emit utterance_end.
// Only the VAD model is needed.
func (e *asrEngine) startPassthrough(ctx context.Context) { e.startMode(ctx, true) }

func (e *asrEngine) startMode(ctx context.Context, passthrough bool) {
	if e.state != "idle" {
		return
	}
	if passthrough {
		if !fileExists(e.vadPath) {
			vadPath, err := ensureVADModel(ctx, e.modelRoot, e.ev)
			if err != nil {
				e.ev.errorf("model setup: %v", err)
				return
			}
			e.vadPath = vadPath
		}
	} else {
		if !fileExists(e.vadPath) {
			vadPath, err := ensureVADModel(ctx, e.modelRoot, e.ev)
			if err != nil {
				e.ev.errorf("model setup: %v", err)
				return
			}
			e.vadPath = vadPath
		}
		if e.asrDir == "" {
			asrDir, err := ensureSenseVoice(ctx, e.modelRoot, e.ev)
			if err != nil {
				e.ev.errorf("model setup: %v", err)
				return
			}
			e.asrDir = asrDir
		}
		if e.rec == nil {
			if err := e.loadRecognizer(); err != nil {
				e.ev.errorf("%v", err)
				return
			}
		}
	}
	if err := e.loadVAD(); err != nil {
		e.ev.errorf("%v", err)
		return
	}
	e.passthrough = passthrough
	e.resetAccum()
	// drop stale chunks from a previous session
	for {
		select {
		case <-e.cap.chunks:
		default:
			goto drained
		}
	}
drained:
	if err := e.cap.start(); err != nil {
		e.ev.errorf("capture: %v", err)
		return
	}
	e.listenSince = time.Now()
	e.setState("listening")
}

func (e *asrEngine) resetAccum() {
	e.accumText = ""
	e.accumSpeech = 0
	e.speechSeen = false
}

// stop ends the session, flushing and decoding any pending speech.
func (e *asrEngine) stop() {
	if e.state == "idle" {
		return
	}
	if e.vad != nil {
		e.vad.Flush()
	}
	e.drainSegments()
	e.cap.stop()
	if e.passthrough {
		if e.speechSeen {
			e.ev.utteranceEnd()
		}
	} else if e.accumText != "" {
		e.ev.final(e.accumText)
	}
	e.resetAccum()
	e.setState("idle")
}

func (e *asrEngine) decode(samples []float32) string {
	if e.rec == nil || len(samples) == 0 {
		return ""
	}
	stream := sherpa.NewOfflineStream(e.rec)
	defer sherpa.DeleteOfflineStream(stream)
	stream.AcceptWaveform(sampleRate, samples)
	e.rec.Decode(stream)
	result := stream.GetResult()
	if result == nil {
		return ""
	}
	return result.Text
}

// drainSegments decodes all finished VAD segments and appends their text.
// Passthrough mode only tracks segment duration (for max_segment); the PCM
// already streamed out as audio events.
func (e *asrEngine) drainSegments() {
	if e.vad == nil {
		return
	}
	for !e.vad.IsEmpty() {
		seg := e.vad.Front()
		e.vad.Pop()
		e.accumSpeech += time.Duration(len(seg.Samples)) * time.Second / sampleRate
		if e.passthrough {
			continue
		}
		if text := e.decode(seg.Samples); text != "" {
			e.accumText += text
			e.ev.partial(e.accumText)
		}
	}
}

func (e *asrEngine) finalize() {
	e.drainSegments()
	if e.passthrough {
		e.ev.utteranceEnd()
	} else if e.accumText != "" {
		e.ev.final(e.accumText)
	}
	e.resetAccum()
	e.listenSince = time.Now()
	e.setState("listening")
}

// onChunk feeds one audio chunk through the VAD state machine. A nil chunk
// is a timer tick: only timeout checks run.
func (e *asrEngine) onChunk(samples []float32, now time.Time) {
	if e.state == "idle" || e.vad == nil {
		return
	}

	speech := false
	if len(samples) > 0 {
		if e.passthrough {
			e.ev.audio(pcmBytes(samples))
		}
		e.vad.AcceptWaveform(samples)
		speech = e.vad.IsSpeech()
		if speech {
			e.lastSpeech = now
			e.speechSeen = true
			e.setState("speech")
		} else if e.speechSeen && e.state == "speech" {
			e.setState("silence")
		}
		e.drainSegments()
	}

	if !e.speechSeen {
		if e.params.noSpeechTimeout > 0 && now.Sub(e.listenSince) > time.Duration(e.params.noSpeechTimeout*float64(time.Second)) {
			e.ev.errorf("no speech detected within %.0fs", e.params.noSpeechTimeout)
			e.cap.stop()
			e.resetAccum()
			e.setState("idle")
		}
		return
	}

	trailing := time.Duration(e.params.trailingSilence * float64(time.Second))
	maxSeg := time.Duration(e.params.maxSegment * float64(time.Second))
	if !speech && now.Sub(e.lastSpeech) >= trailing {
		e.finalize()
	} else if e.accumSpeech >= maxSeg {
		e.vad.Flush()
		e.finalize()
	}
}

func (e *asrEngine) setModel(path, kind string) {
	if path == "" {
		e.asrDir = ""
		e.asrKind = ""
		return
	}
	k := kind
	if k == "" {
		k = "sensevoice"
	}
	switch k {
	case "sensevoice", "sensevoice-int8", "paraformer":
	default:
		e.ev.errorf("set_model: unknown model kind: %s", k)
		return
	}
	if _, _, err := asrModelPaths(path, k); err != nil {
		e.ev.errorf("set_model: %v", err)
		return
	}
	e.asrDir = path
	e.asrKind = kind
	if err := e.loadRecognizer(); err != nil {
		e.ev.errorf("set_model: %v", err)
	}
}

func (e *asrEngine) setVADParams(cmd Command) {
	p := e.params
	if cmd.Threshold != nil {
		p.threshold = *cmd.Threshold
	}
	if cmd.MinSilence != nil {
		p.minSilence = *cmd.MinSilence
	}
	if cmd.MinSpeech != nil {
		p.minSpeech = *cmd.MinSpeech
	}
	if cmd.TrailingSilence != nil {
		p.trailingSilence = *cmd.TrailingSilence
	}
	if cmd.MaxSegment != nil {
		p.maxSegment = *cmd.MaxSegment
	}
	if cmd.NoSpeechTimeout != nil {
		p.noSpeechTimeout = *cmd.NoSpeechTimeout
	}
	e.params = p

	// threshold/min silence/min speech are baked into the sherpa VAD at
	// construction time, so rebuild it if a session is active.
	if e.state != "idle" && e.vadPath != "" {
		if err := e.loadVAD(); err != nil {
			e.ev.errorf("set_vad_params: %v", err)
		}
	}
}

func (e *asrEngine) close() {
	e.stop()
	if e.vad != nil {
		sherpa.DeleteVoiceActivityDetector(e.vad)
		e.vad = nil
	}
	if e.rec != nil {
		sherpa.DeleteOfflineRecognizer(e.rec)
		e.rec = nil
	}
}

// decodeFile recognizes a wav file and emits the result as a final event.
// Used by the -decode smoke-test flag.
func decodeFile(path, modelRoot string, ev *eventWriter) {
	ctx := context.Background()
	asrDir, _, err := ensureModels(ctx, modelRoot, ev)
	if err != nil {
		ev.errorf("model setup: %v", err)
		os.Exit(1)
	}
	eng := newASREngine(ev, modelRoot)
	eng.asrDir = asrDir
	defer eng.close()
	if err := eng.loadRecognizer(); err != nil {
		ev.errorf("%v", err)
		os.Exit(1)
	}
	wave := sherpa.ReadWave(path)
	if wave == nil {
		ev.errorf("cannot read wav: %s", path)
		os.Exit(1)
	}
	if wave.SampleRate != sampleRate {
		ev.errorf("wav must be %d Hz, got %d", sampleRate, wave.SampleRate)
		os.Exit(1)
	}
	ev.final(eng.decode(wave.Samples))
}
