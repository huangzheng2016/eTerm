package voice

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Volcano Engine speech protocol (openspeech.bytedance.com sauc/bigmodel_async):
// 4-byte header, optional i32 sequence, optional i32 event/error code,
// u32 payload size, gzip-compressed payload.

const (
	msgFullClientRequest  = 0x1
	msgAudioOnlyRequest   = 0x2
	msgFullServerResponse = 0x9
	msgErrorResponse      = 0xf

	flagPosSequence     = 0x1
	flagNegSequence     = 0x2
	flagNegWithSequence = 0x3

	serialJSON = 0x1
	serialNone = 0x0

	compressGzip = 0x1
)

func frameHeader(msgType, flags, serialization, compression byte) [4]byte {
	return [4]byte{0x11, msgType<<4 | flags, serialization<<4 | compression, 0x00}
}

func gzipData(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(payload); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipData(payload []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func buildFrame(msgType, flags, serialization byte, payload []byte, seq int32) ([]byte, error) {
	compressed, err := gzipData(payload)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 0, 12+len(compressed))
	hdr := frameHeader(msgType, flags, serialization, compressGzip)
	frame = append(frame, hdr[:]...)
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], uint32(seq))
	frame = append(frame, tmp[:]...)
	binary.BigEndian.PutUint32(tmp[:], uint32(len(compressed)))
	frame = append(frame, tmp[:]...)
	frame = append(frame, compressed...)
	return frame, nil
}

func volcanoLanguage(lang string) string {
	switch lang {
	case "", "multi", "zh", "zh-CN":
		return "zh-CN"
	case "en", "en-US":
		return "en-US"
	case "ja", "ja-JP":
		return "ja-JP"
	case "ko", "ko-KR":
		return "ko-KR"
	default:
		return lang
	}
}

func buildFullClientRequest(cfg VolcanoConfig, seq int32) ([]byte, error) {
	rate := cfg.SampleRate
	if rate == 0 {
		rate = 16000
	}
	payload := map[string]any{
		"user": map[string]any{"uid": "eterm"},
		"audio": map[string]any{
			"format":   "pcm",
			"codec":    "raw",
			"rate":     rate,
			"bits":     16,
			"channel":  1,
			"language": volcanoLanguage(cfg.Language),
		},
		"request": map[string]any{
			"model_name":      "bigmodel",
			"enable_itn":      cfg.SmartFormat,
			"enable_ddc":      false,
			"enable_punc":     cfg.SmartFormat,
			"show_utterances": true,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return buildFrame(msgFullClientRequest, flagPosSequence, serialJSON, body, seq)
}

// buildAudioFrame packs one PCM chunk. A negative seq marks the final frame.
func buildAudioFrame(chunk []byte, seq int32) ([]byte, error) {
	flags := byte(flagPosSequence)
	if seq < 0 {
		flags = flagNegWithSequence
	}
	return buildFrame(msgAudioOnlyRequest, flags, serialNone, chunk, seq)
}

type serverEvent struct {
	isError bool
	msg     string // error message
	text    string // transcript
	final   bool
}

func parseServerFrame(frame []byte) (*serverEvent, error) {
	if len(frame) < 8 {
		return nil, fmt.Errorf("volcano frame too short: %d", len(frame))
	}
	headerSize := int(frame[0]&0x0f) * 4
	if len(frame) < headerSize+4 {
		return nil, fmt.Errorf("volcano frame header incomplete")
	}
	msgType := frame[1] >> 4
	flags := frame[1] & 0x0f
	serialization := frame[2] >> 4
	compression := frame[2] & 0x0f

	offset := headerSize
	var seq int32
	hasSeq := flags&flagPosSequence != 0
	if hasSeq {
		if len(frame) < offset+4 {
			return nil, fmt.Errorf("volcano frame sequence incomplete")
		}
		seq = int32(binary.BigEndian.Uint32(frame[offset:]))
		offset += 4
	}
	isLast := flags&flagNegSequence != 0
	if flags&0x04 != 0 {
		if len(frame) < offset+4 {
			return nil, fmt.Errorf("volcano frame event incomplete")
		}
		offset += 4
	}

	var errCode int32
	if msgType == msgErrorResponse {
		if len(frame) < offset+8 {
			return nil, fmt.Errorf("volcano error frame incomplete")
		}
		errCode = int32(binary.BigEndian.Uint32(frame[offset:]))
		offset += 4
	}

	if len(frame) < offset+4 {
		return nil, fmt.Errorf("volcano frame payload size incomplete")
	}
	payloadSize := int(binary.BigEndian.Uint32(frame[offset:]))
	offset += 4

	if payloadSize == 0 && msgType == msgErrorResponse {
		return &serverEvent{isError: true, msg: fmt.Sprintf("volcano ASR error %d", errCode)}, nil
	}
	if len(frame) < offset+payloadSize {
		return nil, fmt.Errorf("volcano frame payload incomplete")
	}
	payload := frame[offset : offset+payloadSize]
	if compression == compressGzip {
		var err error
		payload, err = gunzipData(payload)
		if err != nil {
			return nil, fmt.Errorf("volcano gunzip: %w", err)
		}
	}

	if serialization != serialJSON {
		if msgType == msgErrorResponse {
			return &serverEvent{isError: true, msg: fmt.Sprintf("volcano ASR error %d", errCode)}, nil
		}
		return nil, nil
	}

	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, fmt.Errorf("volcano response JSON: %w", err)
	}

	if msgType == msgErrorResponse || jsonCodeIsError(value) {
		return &serverEvent{isError: true, msg: responseMessage(value, errCode)}, nil
	}
	if msgType != msgFullServerResponse {
		return nil, nil
	}

	text := extractTranscript(value)
	if text == "" {
		return nil, nil
	}
	return &serverEvent{text: text, final: isLast || (hasSeq && seq < 0)}, nil
}

func jsonCodeIsError(value map[string]any) bool {
	code, ok := value["code"].(float64)
	if !ok {
		return false
	}
	return code != 0 && code != 20000000
}

func responseMessage(value map[string]any, errCode int32) string {
	if s, ok := value["message"].(string); ok && s != "" {
		return s
	}
	if s, ok := value["error"].(string); ok && s != "" {
		return s
	}
	if m, ok := value["error"].(map[string]any); ok {
		if s, ok := m["message"].(string); ok && s != "" {
			return s
		}
	}
	return fmt.Sprintf("volcano ASR error %d", errCode)
}

func extractTranscript(value map[string]any) string {
	result, _ := value["result"].(map[string]any)
	if result == nil {
		return ""
	}
	if s, ok := result["text"].(string); ok && s != "" {
		return s
	}
	utterances, _ := result["utterances"].([]any)
	if len(utterances) == 0 {
		return ""
	}
	last, _ := utterances[len(utterances)-1].(map[string]any)
	if s, ok := last["text"].(string); ok {
		return s
	}
	return ""
}
