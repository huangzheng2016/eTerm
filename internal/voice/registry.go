package voice

import (
	"fmt"
	"sort"
	"sync"
)

type ParamSpec struct {
	Key      string
	Label    string
	Secret   bool
	Required bool
	Default  string
}

type FeedDeps struct {
	VAD                VADParams
	OnDownloadProgress func(pct float64)
}

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

func EngineDescriptorByID(id string) (EngineDescriptor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[id]
	return d, ok
}

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

func FirstMissingParam(d EngineDescriptor, params map[string]string) string {
	for _, p := range d.Params {
		if p.Required && params[p.Key] == "" {
			return p.Label
		}
	}
	return ""
}
