package sshview

import (
	"sync"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestPlainTranscriptConcurrentWithWrite(t *testing.T) {
	m := &Model{emu: vt.NewEmulator(80, 24)}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			m.writeEmulator([]byte("output line\r\n"))
		}
	}()
	for i := 0; i < 300; i++ {
		_ = m.PlainTranscript(4096)
		_ = m.ANSITranscript(4096)
	}
	wg.Wait()
}
