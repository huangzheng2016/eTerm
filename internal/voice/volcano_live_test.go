package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestVolcanoLiveRecognition dials the real Volcano endpoint and recognizes a
// wav file of 16kHz 16bit mono PCM. It runs only when ETERM_VOLCANO_API_KEY is
// set; ETERM_VOLCANO_TEST_WAV overrides the default /tmp/eterm_volcano_test.wav,
// ETERM_VOLCANO_URL overrides the endpoint (e.g. bigmodel_nostream).
func TestVolcanoLiveRecognition(t *testing.T) {
	apiKey := os.Getenv("ETERM_VOLCANO_API_KEY")
	if apiKey == "" {
		t.Skip("ETERM_VOLCANO_API_KEY not set")
	}
	wavPath := os.Getenv("ETERM_VOLCANO_TEST_WAV")
	if wavPath == "" {
		wavPath = "/tmp/eterm_volcano_test.wav"
	}
	pcm, err := readWavPCM(wavPath)
	if err != nil {
		t.Skipf("test audio: %v", err)
	}

	url := os.Getenv("ETERM_VOLCANO_URL")
	rid := os.Getenv("ETERM_VOLCANO_RESOURCE_ID")
	if rid == "" {
		rid = ResourceIDSeedASR
	}
	text := recognizeLive(t, VolcanoConfig{APIKey: apiKey, ResourceID: rid, URL: url, SampleRate: 16000, SmartFormat: true}, pcm)
	t.Logf("recognized: %s", text)
}

func recognizeLive(t *testing.T, cfg VolcanoConfig, pcm []byte) string {
	t.Helper()
	eng := NewVolcanoEngine(cfg)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for off := 0; off < len(pcm); off += 3200 {
		end := off + 3200
		if end > len(pcm) {
			end = len(pcm)
		}
		if err := eng.WriteAudio(pcm[off:end]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := eng.Stop(); err != nil {
		t.Fatal(err)
	}
	ev := waitVolcanoEvent(t, eng, func(ev Event) bool { return ev.Type == EventFinal })
	if strings.TrimSpace(ev.Text) == "" {
		t.Fatal("empty final transcript")
	}
	eng.Close()
	return ev.Text
}

func readWavPCM(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a wav file")
	}
	var pcm []byte
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4:]))
		body := data[off+8:]
		if size > len(body) {
			size = len(body)
		}
		if id == "data" {
			pcm = body[:size]
		}
		off += 8 + size
		if size%2 == 1 {
			off++
		}
	}
	if pcm == nil {
		return nil, fmt.Errorf("no data chunk in %s", path)
	}
	return pcm, nil
}
