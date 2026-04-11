package security

import "fmt"

type SecureBytes struct {
	data []byte
}

func New(data []byte) *SecureBytes {
	cp := make([]byte, len(data))
	copy(cp, data)
	return &SecureBytes{data: cp}
}

func (s *SecureBytes) Bytes() []byte {
	return s.data
}

func (s *SecureBytes) Len() int {
	return len(s.data)
}

func (s *SecureBytes) Clear() {
	ClearBytes(s.data)
}

func (s *SecureBytes) String() string {
	return fmt.Sprintf("SecureBytes{len=%d}", len(s.data))
}

func ClearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
