package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// Integration test for the VAD state machine with the real silero model and
// SenseVoice ASR. Skipped unless VOICEHELPER_TEST_MODELS points at a model
// dir containing silero_vad.onnx and the SenseVoice model dir.
func TestVADStateMachine(t *testing.T) {
	root := os.Getenv("VOICEHELPER_TEST_MODELS")
	if root == "" {
		t.Skip("set VOICEHELPER_TEST_MODELS to run")
	}
	vadPath := filepath.Join(root, "silero_vad.onnx")
	asrDir := filepath.Join(root, senseVoiceDir)
	if !fileExists(vadPath) {
		t.Skip("no silero_vad.onnx")
	}
	if _, _, err := asrModelPaths(asrDir); err != nil {
		t.Skipf("no ASR model: %v", err)
	}
	wavPath := os.Getenv("VOICEHELPER_TEST_WAV")
	if wavPath == "" {
		t.Skip("set VOICEHELPER_TEST_WAV to a 16kHz speech wav")
	}
	wave := sherpa.ReadWave(wavPath)
	if wave == nil || wave.SampleRate != sampleRate {
		t.Skip("test wav must be 16kHz")
	}

	var buf bytes.Buffer
	ev := newEventWriter(&buf)
	eng := newASREngine(ev, root)
	defer eng.close()
	eng.asrDir = asrDir
	eng.vadPath = vadPath
	if err := eng.loadRecognizer(); err != nil {
		t.Fatal(err)
	}
	if err := eng.loadVAD(); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	eng.listenSince = now
	eng.state = "listening"

	chunk := 320 // 20ms at 16kHz
	advance := func() { now = now.Add(20 * time.Millisecond) }
	silence := make([]float32, chunk)

	// 6s of silence: no-speech timeout (default 5s) must cancel to idle
	for i := 0; i < 300; i++ {
		advance()
		eng.onChunk(silence, now)
	}
	if eng.state != "idle" {
		t.Fatalf("expected idle after no-speech timeout, got %s", eng.state)
	}

	// new session: feed the speech wav in 20ms chunks. Disable the
	// no-speech timeout here: a finalize mid-wav would otherwise start a
	// fresh 5s window that the remaining silence can exceed.
	eng.params.noSpeechTimeout = 3600
	eng.listenSince = now
	eng.state = "listening"
	sawSpeech := false
	for off := 0; off < len(wave.Samples); off += chunk {
		end := off + chunk
		if end > len(wave.Samples) {
			end = len(wave.Samples)
		}
		advance()
		eng.onChunk(wave.Samples[off:end], now)
		if eng.state == "speech" {
			sawSpeech = true
		}
	}
	if !sawSpeech {
		t.Fatal("VAD never detected speech in the test wav")
	}

	// 2s of trailing silence: must finalize with decoded text
	for i := 0; i < 100; i++ {
		advance()
		eng.onChunk(silence, now)
	}

	var finals []string
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		if e.Type == "final" {
			finals = append(finals, e.Text)
		}
	}
	joined := strings.ToLower(strings.Join(finals, " "))
	if !strings.Contains(joined, "hello") {
		t.Fatalf("final events %v do not contain 'hello'", finals)
	}
	if eng.state != "listening" {
		t.Fatalf("expected listening after finalize, got %s", eng.state)
	}
}
