package syncd

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
)

type PeerInfo struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	LastSeen time.Time `json:"last_seen"`
}

type PeerConn struct {
	PeerInfo
	Send chan relay.Frame
}

type PeerRegistry struct {
	mu      sync.Mutex
	tenants map[string]map[string]*PeerConn
}

func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{tenants: make(map[string]map[string]*PeerConn)}
}

func (r *PeerRegistry) Register(tenant string, p PeerInfo, send chan relay.Frame) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenants[tenant] == nil {
		r.tenants[tenant] = make(map[string]*PeerConn)
	}
	if p.LastSeen.IsZero() {
		p.LastSeen = time.Now()
	}
	baseID := p.ID
	for i := 2; r.tenants[tenant][p.ID] != nil; i++ {
		p.ID = baseID + "#" + time.Now().Format("150405") + "-" + strconv.Itoa(i)
	}
	r.tenants[tenant][p.ID] = &PeerConn{PeerInfo: p, Send: send}
	return p.ID
}

func (r *PeerRegistry) Unregister(tenant, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenants[tenant] == nil {
		return
	}
	delete(r.tenants[tenant], id)
	if len(r.tenants[tenant]) == 0 {
		delete(r.tenants, tenant)
	}
}

func (r *PeerRegistry) List(tenant string) []PeerInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	peers := r.tenants[tenant]
	out := make([]PeerInfo, 0, len(peers))
	for _, p := range peers {
		out = append(out, p.PeerInfo)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *PeerRegistry) Get(tenant, id string) (*PeerConn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenants[tenant] == nil {
		return nil, false
	}
	p, ok := r.tenants[tenant][id]
	return p, ok
}
