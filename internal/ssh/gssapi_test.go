package ssh

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCCachePathFile(t *testing.T) {
	resolved, err := resolveCCachePath("FILE:/tmp/krb5cc_test")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.path != "/tmp/krb5cc_test" {
		t.Fatalf("got path %q", resolved.path)
	}
	if len(resolved.cleanupPaths) != 0 {
		t.Fatalf("unexpected cleanup paths: %#v", resolved.cleanupPaths)
	}
}

func TestResolveCCachePathUnsupportedSchemes(t *testing.T) {
	for _, cache := range []string{"DIR:/tmp/cc", "KCM:123"} {
		_, err := resolveCCachePath(cache)
		if err == nil {
			t.Fatalf("expected error for %q", cache)
		}
	}
}

func TestResolveCCachePathDarwinAPIStagesWithFallback(t *testing.T) {
	oldGOOS := gssapiGOOS
	oldRun := gssapiRunCommand
	gssapiGOOS = "darwin"
	t.Cleanup(func() {
		gssapiGOOS = oldGOOS
		gssapiRunCommand = oldRun
	})

	var calls [][]string
	gssapiRunCommand = func(name string, args ...string) ([]byte, error) {
		if name != "kcc" {
			t.Fatalf("unexpected command %q", name)
		}
		call := append([]string(nil), args...)
		calls = append(calls, call)
		dest := trimFileScheme(args[len(args)-1])
		if len(calls) == 1 {
			return []byte("first failed"), errors.New("exit 1")
		}
		if err := os.WriteFile(dest, []byte("cache"), 0600); err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}

	resolved, err := resolveCCachePath("API:test-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanupAll(resolved.cleanupPaths); err != nil {
			t.Fatal(err)
		}
	}()

	if len(calls) != 2 {
		t.Fatalf("got %d calls", len(calls))
	}
	if calls[0][0] != "copy_cred_cache" || calls[0][1] != "API:test-cache" {
		t.Fatalf("unexpected first call: %#v", calls[0])
	}
	if calls[0][2] == calls[1][2] {
		t.Fatalf("expected FILE and plain path attempts, got %#v", calls)
	}
	if _, err := os.Stat(resolved.path); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCCachePathDarwinDefaultStages(t *testing.T) {
	oldGOOS := gssapiGOOS
	oldRun := gssapiRunCommand
	gssapiGOOS = "darwin"
	t.Cleanup(func() {
		gssapiGOOS = oldGOOS
		gssapiRunCommand = oldRun
	})
	t.Setenv("KRB5CCNAME", "")

	var calls [][]string
	gssapiRunCommand = func(name string, args ...string) ([]byte, error) {
		call := append([]string(nil), args...)
		calls = append(calls, call)
		dest := trimFileScheme(args[len(args)-1])
		if err := os.WriteFile(dest, []byte("cache"), 0600); err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}

	resolved, err := resolveCCachePath("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanupAll(resolved.cleanupPaths); err != nil {
			t.Fatal(err)
		}
	}()

	if len(calls) != 1 {
		t.Fatalf("got %d calls", len(calls))
	}
	if len(calls[0]) != 2 || calls[0][0] != "copy_cred_cache" {
		t.Fatalf("unexpected default-stage call: %#v", calls[0])
	}
}

func TestLoadKrb5ConfSynthesizesWhenNoFileExists(t *testing.T) {
	oldPath := gssapiDefaultKrb5ConfPath
	gssapiDefaultKrb5ConfPath = filepath.Join(t.TempDir(), "missing-krb5.conf")
	t.Cleanup(func() {
		gssapiDefaultKrb5ConfPath = oldPath
	})
	t.Setenv("KRB5_CONFIG", "")

	cfg, err := loadKrb5Conf("", "EXAMPLE.COM")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LibDefaults.DefaultRealm != "EXAMPLE.COM" {
		t.Fatalf("got realm %q", cfg.LibDefaults.DefaultRealm)
	}
	if !cfg.LibDefaults.DNSLookupKDC {
		t.Fatal("expected DNSLookupKDC")
	}
	if !cfg.LibDefaults.DNSLookupRealm {
		t.Fatal("expected DNSLookupRealm")
	}
}

func TestDeleteSecContextRemovesCleanupPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ccache-dir")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}

	client := &gssAPIClient{cleanupPaths: []string{path}}
	if err := client.DeleteSecContext(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected cleanup, got err=%v", err)
	}
}
