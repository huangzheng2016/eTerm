package sshview

import "testing"

func TestTranscriptLimitIsFourMiB(t *testing.T) {
	if MaxTranscriptBytes != 4*1024*1024 {
		t.Fatalf("MaxTranscriptBytes = %d", MaxTranscriptBytes)
	}
}
