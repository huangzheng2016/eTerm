package sync

// Transport abstracts the sync transport (HTTP, possibly over an SSH tunnel).
type Transport interface {
	Ping() error
	Pull(sinceRev int64) ([]SyncRecord, int64, error)
	Push(records []SyncRecord) error
	Close() error
}
