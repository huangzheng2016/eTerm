package voice

import (
	"fmt"
	"sort"
	"sync"
)

// ParamSpec describes one engine parameter row in the settings panel.
type ParamSpec struct {
	Key      string // persisted inside the per-engine params blob
	Label    string // panel row label
	Secret   bool   // masked display in the panel
	Required bool   // counts toward readiness
	Default  string
}

// FeedDeps carries the assembly dependencies an engine needs from the app:
// endpoint tuning and progress reporting for a helper-driven audio feed.
type FeedDeps struct {
	VAD                VADParams
	OnDownloadProgress func(pct float64)
}

// EngineDescriptor describes one registered engine: panel metadata, the
// parameter-readiness check behind ctrl+r gating, and the engine factory.
// params holds the persisted per-engine values (ParamSpec defaults applied).
type EngineDescriptor struct {
	ID     string
	Label  string
	Params []ParamSpec
	Ready  func(params map[string]string) bool
	New    func(params map[string]string, feed FeedDeps) (Engine, error)
}

var (
	registryMu sync.RWMutex
	registry   = map[string]EngineDescriptor{}
)

// RegisterEngine adds an engine descriptor; engines self-register via init().
// An empty or duplicate ID panics (a programming error caught at startup).
func RegisterEngine(d EngineDescriptor) {
	if d.ID == "" || d.New == nil || d.Ready == nil {
		panic(fmt.Sprintf("voice: invalid engine descriptor %+v", d))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[d.ID]; dup {
		panic("voice: duplicate engine id " + d.ID)
	}
	registry[d.ID] = d
}

// EngineDescriptorByID resolves a registered engine.
func EngineDescriptorByID(id string) (EngineDescriptor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[id]
	return d, ok
}

// EngineDescriptors lists all registered engines sorted by ID.
func EngineDescriptors() []EngineDescriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]EngineDescriptor, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FirstMissingParam returns the label of the first required parameter
// without a value, for setup-incomplete guidance.
func FirstMissingParam(d EngineDescriptor, params map[string]string) string {
	for _, p := range d.Params {
		if p.Required && params[p.Key] == "" {
			return p.Label
		}
	}
	return ""
}
