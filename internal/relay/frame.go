package relay

import (
	"encoding/binary"
	"fmt"
)

type FrameType byte

const (
	FrameHello    FrameType = 0x01
	FramePeerList FrameType = 0x02
	FrameHostList FrameType = 0x03
	FrameOpen     FrameType = 0x10
	FrameOpenOK   FrameType = 0x11
	FrameOpenErr  FrameType = 0x12
	FrameData     FrameType = 0x20
	FrameResize   FrameType = 0x21
	FrameClose    FrameType = 0x22
	FramePing     FrameType = 0x30
	FramePong     FrameType = 0x31
)

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
	PeerID     string `json:"peer_id"`
	Target     string `json:"target"`
	HostSyncID string `json:"host_sync_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Cols       int    `json:"cols,omitempty"`
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
