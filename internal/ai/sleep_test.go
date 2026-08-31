package ai

import (
	"context"
	"testing"
	"time"
)

func TestSleepClampsAndSleeps(t *testing.T) {
	start := time.Now()
	out, err := sleep(context.Background(), &SleepInput{Seconds: 0}) // clamped to 1
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 900*time.Millisecond {
		t.Fatalf("sleep(0) must clamp to 1s, returned after %v", elapsed)
	}
	if out.SleptSeconds < 0.9 {
		t.Fatalf("slept_seconds: got %v, want ~1", out.SleptSeconds)
	}
}

func TestSleepReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	out, err := sleep(ctx, &SleepInput{Seconds: 10000}) // clamped to 600, canceled early
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("sleep must return promptly on cancel, took %v", elapsed)
	}
	if out.SleptSeconds > 10 {
		t.Fatalf("slept_seconds must reflect actual wait: got %v", out.SleptSeconds)
	}
}
