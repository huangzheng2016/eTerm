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

func TestPeerRegistryReplacesDuplicatePeerID(t *testing.T) {
	r := NewPeerRegistry()
	firstSend := make(chan relay.Frame, 1)
	secondSend := make(chan relay.Frame, 1)
	first := r.Register("tenant-a", PeerInfo{ID: "peer", Name: "alpha"}, firstSend)
	second := r.Register("tenant-a", PeerInfo{ID: "peer", Name: "alpha"}, secondSend)

	if first != "peer" || second != "peer" {
		t.Fatalf("registrations = %q, %q; want stable peer id", first, second)
	}
	got := r.List("tenant-a")
	if len(got) != 1 {
		t.Fatalf("got %d peers, want 1", len(got))
	}
	peer, ok := r.Get("tenant-a", "peer")
	if !ok {
		t.Fatal("peer not addressable")
	}
	if peer.Send != secondSend {
		t.Fatal("peer did not point to latest connection")
	}
	r.UnregisterConn("tenant-a", "peer", firstSend)
	if peer, ok := r.Get("tenant-a", "peer"); !ok || peer.Send != secondSend {
		t.Fatal("old connection unregister removed latest peer")
	}
}
