package relay

import (
	"encoding/binary"
	"fmt"
)

type FrameType byte

const (
	FrameHello    FrameType = 0x01
	FrameHelloErr FrameType = 0x04
	FrameOpen     FrameType = 0x10
	FrameOpenOK   FrameType = 0x11
	FrameOpenErr  FrameType = 0x12
	FrameData     FrameType = 0x20
	FrameResize   FrameType = 0x21
	FrameClose    FrameType = 0x22
	FrameAck      FrameType = 0x23
)

// ProtocolVersion is the relay wire version. Peers without a version are v1.
const ProtocolVersion = 2

const (
	TargetLocal      = "local"
	TargetHost       = "host"
	TargetTmuxList   = "tmux-list"
	TargetTmuxNew    = "tmux-new"
	TargetTmuxAttach = "tmux-attach"
	TargetTmuxKill   = "tmux-kill"
	TargetTmuxRename = "tmux-rename"
)

const CloseDaemonDisconnected = "daemon disconnected"

// CloseClientDisconnected is the FrameClose payload syncd sends to the daemon
// when a client connection drops; the daemon keeps the PTY alive for resume.
const CloseClientDisconnected = "client disconnected"

type TmuxSessionInfo struct {
	Name        string `json:"name"`
	CreatedUnix int64  `json:"created_unix"`
	Attached    bool   `json:"attached"`
}

type HelloPayload struct {
	Role    string `json:"role"`
	Tenant  string `json:"tenant"`
	PeerID  string `json:"peer_id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type OpenRequest struct {
	PeerID        string `json:"peer_id"`
	Target        string `json:"target"`
	HostSyncID    string `json:"host_sync_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Rows          int    `json:"rows,omitempty"`
	Cols          int    `json:"cols,omitempty"`
	ResumeFromSeq uint64 `json:"resume_from_seq,omitempty"`
}

const HeaderLen = 10

const MaxWebSocketMessageBytes = 1 << 20

type Frame struct {
	Type     FrameType
	Flags    byte
	StreamID uint32
	Payload  []byte
}

func Encode(f Frame) []byte {
	out := make([]byte, HeaderLen+len(f.Payload))
	out[0] = byte(f.Type)
	out[1] = f.Flags
	binary.BigEndian.PutUint32(out[2:6], f.StreamID)
	binary.BigEndian.PutUint32(out[6:10], uint32(len(f.Payload)))
	copy(out[HeaderLen:], f.Payload)
	return out
}

func Decode(b []byte) (Frame, error) {
	if len(b) < HeaderLen {
		return Frame{}, fmt.Errorf("frame too short")
	}
	n := binary.BigEndian.Uint32(b[6:10])
	if int(n) != len(b)-HeaderLen {
		return Frame{}, fmt.Errorf("frame payload length mismatch")
	}
	payload := make([]byte, int(n))
	copy(payload, b[HeaderLen:])
	return Frame{
		Type:     FrameType(b[0]),
		Flags:    b[1],
		StreamID: binary.BigEndian.Uint32(b[2:6]),
		Payload:  payload,
	}, nil
}

func ResizePayload(rows, cols int) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint16(out[0:2], uint16(rows))
	binary.BigEndian.PutUint16(out[2:4], uint16(cols))
	return out
}

func ParseResize(b []byte) (int, int, error) {
	if len(b) != 4 {
		return 0, 0, fmt.Errorf("resize payload must be 4 bytes")
	}
	return int(binary.BigEndian.Uint16(b[0:2])), int(binary.BigEndian.Uint16(b[2:4])), nil
}

// In protocol v2, daemon -> client FrameData payloads are prefixed with an
// 8-byte big-endian sequence: the absolute byte offset of the data in the
// stream. Client -> daemon FrameData payloads are raw terminal input.
const dataSeqLen = 8

func DataPayload(seq uint64, data []byte) []byte {
	out := make([]byte, dataSeqLen+len(data))
	binary.BigEndian.PutUint64(out[:dataSeqLen], seq)
	copy(out[dataSeqLen:], data)
	return out
}

func ParseData(b []byte) (uint64, []byte, error) {
	if len(b) < dataSeqLen {
		return 0, nil, fmt.Errorf("data payload must be at least %d bytes", dataSeqLen)
	}
	return binary.BigEndian.Uint64(b[:dataSeqLen]), b[dataSeqLen:], nil
}

// FrameAck payloads are the 8-byte big-endian cumulative offset the client
// has consumed (the next sequence it expects).
func AckPayload(ack uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, ack)
	return out
}

func ParseAck(b []byte) (uint64, error) {
	if len(b) != 8 {
		return 0, fmt.Errorf("ack payload must be 8 bytes")
	}
	return binary.BigEndian.Uint64(b), nil
}
