package voice

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestBuildFullClientRequest(t *testing.T) {
	cfg := VolcanoConfig{SampleRate: 16000, Language: "zh", SmartFormat: true}
	frame, err := buildFullClientRequest(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}

	if frame[0] != 0x11 || frame[1] != 0x11 || frame[2] != 0x11 || frame[3] != 0x00 {
		t.Fatalf("bad header: % x", frame[:4])
	}
	if seq := int32(binary.BigEndian.Uint32(frame[4:])); seq != 1 {
		t.Fatalf("seq = %d", seq)
	}
	size := int(binary.BigEndian.Uint32(frame[8:]))
	payload, err := gunzipData(frame[12 : 12+size])
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	audio := value["audio"].(map[string]any)
	if audio["format"] != "pcm" || audio["bits"] != float64(16) || audio["channel"] != float64(1) {
		t.Fatalf("audio: %v", audio)
	}
	if audio["rate"] != float64(16000) || audio["language"] != "zh-CN" {
		t.Fatalf("audio: %v", audio)
	}
	req := value["request"].(map[string]any)
	if req["model_name"] != "bigmodel" || req["enable_itn"] != true || req["enable_punc"] != true || req["show_utterances"] != true {
		t.Fatalf("request: %v", req)
	}
}

func TestBuildAudioFrameNegativeSeqIsFinal(t *testing.T) {
	frame, err := buildAudioFrame([]byte{0x01, 0x02}, -2)
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != 0x11 || frame[1] != 0x23 || frame[2] != 0x01 || frame[3] != 0x00 {
		t.Fatalf("bad header: % x", frame[:4])
	}
	if seq := int32(binary.BigEndian.Uint32(frame[4:])); seq != -2 {
		t.Fatalf("seq = %d", seq)
	}
	size := int(binary.BigEndian.Uint32(frame[8:]))
	payload, err := gunzipData(frame[12 : 12+size])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, []byte{0x01, 0x02}) {
		t.Fatalf("payload: %v", payload)
	}
}

func TestBuildAudioFramePositiveSeq(t *testing.T) {
	frame, err := buildAudioFrame([]byte{0xaa}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if frame[1] != 0x21 {
		t.Fatalf("flags: %x", frame[1])
	}
	if seq := int32(binary.BigEndian.Uint32(frame[4:])); seq != 3 {
		t.Fatalf("seq = %d", seq)
	}
}

func serverFrame(t *testing.T, seq int32, payload []byte) []byte {
	t.Helper()
	flags := byte(flagPosSequence)
	if seq < 0 {
		flags = flagNegWithSequence
	}
	frame, err := buildFrame(msgFullServerResponse, flags, serialJSON, payload, seq)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestParseFinalServerResponse(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"result": map[string]any{
			"text": "hello world",
			"utterances": []any{
				map[string]any{"text": "hello world", "definite": true},
			},
		},
	})
	ev, err := parseServerFrame(serverFrame(t, -3, payload))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || !ev.final || ev.text != "hello world" {
		t.Fatalf("event: %+v", ev)
	}
}

func TestParsePartialServerResponse(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"result": map[string]any{"text": "hel"},
	})
	ev, err := parseServerFrame(serverFrame(t, 2, payload))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || ev.final || ev.text != "hel" {
		t.Fatalf("event: %+v", ev)
	}
}

func TestParseFinalWithoutSequenceFlag(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"result": map[string]any{"text": "done"},
	})
	gz, err := gzipData(payload)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 0, 8+len(gz))
	hdr := frameHeader(msgFullServerResponse, flagNegSequence, serialJSON, compressGzip)
	frame = append(frame, hdr[:]...)
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(len(gz)))
	frame = append(frame, tmp[:]...)
	frame = append(frame, gz...)

	ev, err := parseServerFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || !ev.final || ev.text != "done" {
		t.Fatalf("event: %+v", ev)
	}
}

func TestParseErrorResponse(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"message": "invalid resource"})
	gz, err := gzipData(payload)
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 0, 12+len(gz))
	hdr := frameHeader(msgErrorResponse, 0x00, serialJSON, compressGzip)
	frame = append(frame, hdr[:]...)
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(400))
	frame = append(frame, tmp[:]...)
	binary.BigEndian.PutUint32(tmp[:], uint32(len(gz)))
	frame = append(frame, tmp[:]...)
	frame = append(frame, gz...)

	ev, err := parseServerFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || !ev.isError || ev.msg != "invalid resource" {
		t.Fatalf("event: %+v", ev)
	}
}

func TestParseJSONCodeError(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"code": 45000001, "message": "quota exceeded"})
	ev, err := parseServerFrame(serverFrame(t, 5, payload))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || !ev.isError || ev.msg != "quota exceeded" {
		t.Fatalf("event: %+v", ev)
	}
}

func TestParseTruncatedFrame(t *testing.T) {
	if _, err := parseServerFrame([]byte{0x11, 0x91}); err == nil {
		t.Fatal("expected error for short frame")
	}
	frame := serverFrame(t, 1, []byte(`{"result":{"text":"x"}}`))
	if _, err := parseServerFrame(frame[:len(frame)-3]); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestParseEmptyTextReturnsNil(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"result": map[string]any{"text": ""}})
	ev, err := parseServerFrame(serverFrame(t, 1, payload))
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Fatalf("event: %+v", ev)
	}
}

func TestVolcanoLanguage(t *testing.T) {
	for in, want := range map[string]string{
		"": "zh-CN", "multi": "zh-CN", "zh": "zh-CN", "zh-CN": "zh-CN",
		"en": "en-US", "en-US": "en-US",
		"ja": "ja-JP", "ko": "ko-KR", "yue": "yue",
	} {
		if got := volcanoLanguage(in); got != want {
			t.Errorf("volcanoLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSentenceEndApply(t *testing.T) {
	if got := SentenceEndEnter.Apply("hello"); got != "hello\n" {
		t.Fatalf("enter: %q", got)
	}
	if got := SentenceEndSpace.Apply("hello"); got != "hello " {
		t.Fatalf("space: %q", got)
	}
	if got := SentenceEnd("").Apply("hello"); got != "hello" {
		t.Fatalf("default: %q", got)
	}
}
