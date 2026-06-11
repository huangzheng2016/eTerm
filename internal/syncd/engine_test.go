package syncd

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(database)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestPushAssignsUniqueRevisionsUnderConcurrency(t *testing.T) {
	engine := testEngine(t)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := engine.Push([]SyncEntry{{
				SyncID:    fmt.Sprintf("sync-%02d", i),
				Type:      "host",
				Payload:   "{}",
				DeviceID:  "device-a",
				UpdatedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
			}})
			if err != nil {
				t.Errorf("push %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	entries, rev, err := engine.Pull(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("got %d entries, want 20", len(entries))
	}
	if rev != 20 {
		t.Fatalf("got max revision %d, want 20", rev)
	}
	seen := map[int64]bool{}
	for _, entry := range entries {
		if seen[entry.Revision] {
			t.Fatalf("duplicate revision %d", entry.Revision)
		}
		seen[entry.Revision] = true
	}
}
