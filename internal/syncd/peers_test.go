package syncd

import (
	"testing"
	"time"

	"github.com/huangzheng2016/eTerm/internal/relay"
)

func TestPeerRegistryListSortedByTenant(t *testing.T) {
	r := NewPeerRegistry()
	r.Register("tenant-a", PeerInfo{ID: "2", Name: "beta", LastSeen: time.Unix(2, 0)}, make(chan relay.Frame, 1))
	r.Register("tenant-a", PeerInfo{ID: "1", Name: "alpha", LastSeen: time.Unix(1, 0)}, make(chan relay.Frame, 1))
	r.Register("tenant-b", PeerInfo{ID: "3", Name: "aardvark", LastSeen: time.Unix(3, 0)}, make(chan relay.Frame, 1))

	got := r.List("tenant-a")
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("got %#v, want alpha then beta", got)
	}
}

func TestPeerRegistryUnregister(t *testing.T) {
	r := NewPeerRegistry()
	id := r.Register("tenant-a", PeerInfo{ID: "1", Name: "alpha"}, make(chan relay.Frame, 1))
	r.Unregister("tenant-a", "1")

	if len(r.List("tenant-a")) != 0 {
		t.Fatal("peer was not unregistered")
	}
	if _, ok := r.Get("tenant-a", "1"); ok {
		t.Fatal("unregistered peer is still addressable")
	}
	if id != "1" {
		t.Fatalf("id = %q, want 1", id)
	}
}

func TestPeerRegistryAllowsDuplicatePeerIDs(t *testing.T) {
	r := NewPeerRegistry()
	first := r.Register("tenant-a", PeerInfo{ID: "peer", Name: "alpha"}, make(chan relay.Frame, 1))
	second := r.Register("tenant-a", PeerInfo{ID: "peer", Name: "alpha"}, make(chan relay.Frame, 1))

	if first == second {
		t.Fatalf("duplicate registrations used same id %q", first)
	}
	got := r.List("tenant-a")
	if len(got) != 2 {
		t.Fatalf("got %d peers, want 2", len(got))
	}
	if _, ok := r.Get("tenant-a", first); !ok {
		t.Fatalf("first id %q not addressable", first)
	}
	if _, ok := r.Get("tenant-a", second); !ok {
		t.Fatalf("second id %q not addressable", second)
	}
	r.Unregister("tenant-a", first)
	if _, ok := r.Get("tenant-a", second); !ok {
		t.Fatalf("second id %q removed with first", second)
	}
}
