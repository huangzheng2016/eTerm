package syncd

import (
	"encoding/json"
	"os"
)

type stdioRequest struct {
	Method  string           `json:"method"`
	Since   int64            `json:"since,omitempty"`
	Records []syncRecordWire `json:"records,omitempty"`
}

type stdioResponse struct {
	OK       bool             `json:"ok,omitempty"`
	Records  []syncRecordWire `json:"records,omitempty"`
	Revision int64            `json:"revision,omitempty"`
	Error    string           `json:"error,omitempty"`
}

func RunStdio(engine *Engine) error {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for dec.More() {
		var req stdioRequest
		if err := dec.Decode(&req); err != nil {
			return err
		}

		var resp stdioResponse
		switch req.Method {
		case "ping":
			resp.OK = true

		case "pull":
			entries, rev, err := engine.Pull("", req.Since)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Records = entriesToWire(entries)
				resp.Revision = rev
			}

		case "push":
			entries := wireToEntries(req.Records)
			rev, err := engine.Push("", entries)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Revision = rev
			}

		default:
			resp.Error = "unknown method: " + req.Method
		}

		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return nil
}

func entriesToWire(entries []SyncEntry) []syncRecordWire {
	out := make([]syncRecordWire, len(entries))
	for i, e := range entries {
		out[i] = syncRecordWire{
			SyncID: e.SyncID, Type: e.Type, Deleted: e.Deleted,
			DeviceID: e.DeviceID, Meta: e.Meta, Payload: e.Payload, UpdatedAt: e.UpdatedAt,
		}
	}
	return out
}

func wireToEntries(records []syncRecordWire) []SyncEntry {
	out := make([]SyncEntry, len(records))
	for i, r := range records {
		out[i] = SyncEntry{
			SyncID: r.SyncID, Type: r.Type, Deleted: r.Deleted,
			DeviceID: r.DeviceID, Meta: r.Meta, Payload: r.Payload, UpdatedAt: r.UpdatedAt,
		}
	}
	return out
}
