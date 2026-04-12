package syncd

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type syncRecordWire struct {
	SyncID    string    `json:"sync_id"`
	Type      string    `json:"type"`
	Deleted   bool      `json:"deleted"`
	DeviceID  string    `json:"device_id"`
	Payload   string    `json:"payload"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewHTTPHandler(engine *Engine, apiKey string) http.Handler {
	mux := http.NewServeMux()

	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if apiKey != "" {
				h := r.Header.Get("Authorization")
				if !strings.HasPrefix(h, "Bearer ") || subtle.ConstantTimeCompare([]byte(h[7:]), []byte(apiKey)) != 1 {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
			next(w, r)
		}
	}

	mux.HandleFunc("GET /api/v1/ping", auth(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	mux.HandleFunc("GET /api/v1/records", auth(func(w http.ResponseWriter, r *http.Request) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		entries, rev, err := engine.Pull(since)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		records := make([]syncRecordWire, len(entries))
		for i, e := range entries {
			records[i] = syncRecordWire{
				SyncID: e.SyncID, Type: e.Type, Deleted: e.Deleted,
				DeviceID: e.DeviceID, Payload: e.Payload, UpdatedAt: e.UpdatedAt,
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": records, "revision": rev,
		})
	}))

	mux.HandleFunc("POST /api/v1/records", auth(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20) // 16 MB
		var body struct {
			Records []syncRecordWire `json:"records"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		entries := make([]SyncEntry, len(body.Records))
		for i, r := range body.Records {
			entries[i] = SyncEntry{
				SyncID: r.SyncID, Type: r.Type, Deleted: r.Deleted,
				DeviceID: r.DeviceID, Payload: r.Payload, UpdatedAt: r.UpdatedAt,
			}
		}
		rev, err := engine.Push(entries)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]int64{"revision": rev})
	}))

	return mux
}