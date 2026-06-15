package sync

import "testing"

func TestTenantIDFromPassphraseStable(t *testing.T) {
	got := TenantIDFromPassphrase("shared")
	if got == "" {
		t.Fatal("tenant id is empty")
	}
	if got != TenantIDFromPassphrase("shared") {
		t.Fatal("tenant id is not stable")
	}
	if got == TenantIDFromPassphrase("other") {
		t.Fatal("different passphrases produced same tenant id")
	}
}

func TestTenantIDFromPassphraseEmpty(t *testing.T) {
	if TenantIDFromPassphrase("") != "" {
		t.Fatal("empty passphrase should use default tenant")
	}
}
