package syncd

import (
	"strings"
	"testing"
	"time"
)

func TestShareCreateAndGet(t *testing.T) {
	engine := testEngine(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "demo", "", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(share.ID, "shr_") {
		t.Fatalf("id = %q", share.ID)
	}
	if share.Token == "" || share.MaxHours != 4 {
		t.Fatalf("share = %+v", share)
	}
	if d := time.Until(share.ExpiresAt); d < 3*time.Hour || d > 4*time.Hour {
		t.Fatalf("expires in %v", d)
	}
	got, err := engine.GetShareByToken(share.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != share.ID || got.Tenant != "tenant-a" || got.PeerID != "peer-a" || got.Name != "demo" {
		t.Fatalf("got %+v", got)
	}
	if _, err := engine.GetShareByToken("nope"); err != ErrShareNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestShareClampHours(t *testing.T) {
	engine := testEngine(t)
	for _, tc := range []struct{ in, want int }{{0, 4}, {-1, 4}, {1, 1}, {200, 168}} {
		share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if share.MaxHours != tc.want {
			t.Fatalf("max_hours %d -> %d, want %d", tc.in, share.MaxHours, tc.want)
		}
		if d := time.Until(share.ExpiresAt); d < time.Duration(tc.want-1)*time.Hour || d > time.Duration(tc.want)*time.Hour {
			t.Fatalf("max_hours %d expires in %v", tc.in, d)
		}
	}
}

func TestShareExpiredGetDeletes(t *testing.T) {
	engine := testEngine(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.DB.Model(&ShareEntry{}).Where("id = ?", share.ID).Update("expires_at", time.Now().UTC().Add(-time.Hour))
	if _, err := engine.GetShareByToken(share.Token); err != ErrShareNotFound {
		t.Fatalf("err = %v", err)
	}
	var count int64
	engine.DB.Model(&ShareEntry{}).Where("id = ?", share.ID).Count(&count)
	if count != 0 {
		t.Fatal("expired share not deleted")
	}
}

func TestShareRenew(t *testing.T) {
	engine := testEngine(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	engine.DB.Model(&ShareEntry{}).Where("id = ?", share.ID).Update("expires_at", time.Now().UTC().Add(30*time.Minute))
	renewed, err := engine.RenewShare("tenant-a", share.Token)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(renewed.ExpiresAt); d < time.Hour || d > 2*time.Hour {
		t.Fatalf("renewed expires in %v", d)
	}
	if _, err := engine.RenewShare("tenant-b", share.Token); err != ErrShareNotFound {
		t.Fatalf("cross-tenant renew err = %v", err)
	}
}

func TestShareRenewExpired(t *testing.T) {
	engine := testEngine(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.DB.Model(&ShareEntry{}).Where("id = ?", share.ID).Update("expires_at", time.Now().UTC().Add(-time.Hour))
	if _, err := engine.RenewShare("tenant-a", share.Token); err != ErrShareNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestShareDelete(t *testing.T) {
	engine := testEngine(t)
	share, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.DeleteShare("tenant-b", share.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.GetShareByToken(share.Token); err != nil {
		t.Fatalf("cross-tenant delete removed share: %v", err)
	}
	if err := engine.DeleteShare("tenant-a", share.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.GetShareByToken(share.Token); err != ErrShareNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestCleanupExpiredShares(t *testing.T) {
	engine := testEngine(t)
	expired, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := engine.CreateShare("tenant-a", "peer-a", "", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.DB.Model(&ShareEntry{}).Where("id = ?", expired.ID).Update("expires_at", time.Now().UTC().Add(-time.Hour))
	if err := engine.CleanupExpiredShares(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.GetShareByToken(valid.Token); err != nil {
		t.Fatalf("valid share gone: %v", err)
	}
	var count int64
	engine.DB.Model(&ShareEntry{}).Where("id = ?", expired.ID).Count(&count)
	if count != 0 {
		t.Fatal("expired share not cleaned up")
	}
}
