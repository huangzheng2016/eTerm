package components

import (
	"testing"
	"time"
)

func TestToastIgnoresStaleTimeout(t *testing.T) {
	tm := NewToast()
	tm, _ = tm.Show("Connecting...", ToastInfo, time.Second)
	stale := ToastTimeoutMsg{seq: tm.seq}
	tm, _ = tm.Show("Still connecting...", ToastInfo, time.Second)

	tm, _ = tm.Update(stale)

	if !tm.visible {
		t.Fatal("stale timeout hid current toast")
	}
	if tm.message != "Still connecting..." {
		t.Fatalf("message = %q", tm.message)
	}
}
