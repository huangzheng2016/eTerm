package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPPushSplitsIntoBatches(t *testing.T) {
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Records []SyncRecord `json:"records"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		batches = append(batches, len(body.Records))
		json.NewEncoder(w).Encode(map[string]int64{"revision": 1})
	}))
	defer srv.Close()

	payload := strings.Repeat("x", 1<<20)
	records := make([]SyncRecord, 10)
	for i := range records {
		records[i] = SyncRecord{
			SyncID:    strings.Repeat("s", 8) + string(rune('a'+i)),
			Type:      TypeSnippet,
			Payload:   payload,
			UpdatedAt: time.Now(),
		}
	}

	tr := NewHTTPTransport(srv.URL, "key")
	if err := tr.Push(records); err != nil {
		t.Fatal(err)
	}

	total := 0
	for _, n := range batches {
		total += n
	}
	if total != len(records) {
		t.Fatalf("server got %d records, want %d", total, len(records))
	}
	if len(batches) < 2 {
		t.Fatalf("got %d batch(es), want at least 2", len(batches))
	}
}
