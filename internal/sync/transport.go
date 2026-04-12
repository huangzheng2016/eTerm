package sync

// Transport abstracts SSH stdio and HTTP/HTTPS sync transports.
type Transport interface {
	Ping() error
	Pull(sinceRev int64) ([]SyncRecord, int64, error)
	Push(records []SyncRecord) (int64, error)
	Close() error
}
