package sshconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseSSHConfigGSSAPIFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	data := []byte(`
Host kerberos
  HostName kerberos.example.com
  User alice
  GSSAPIAuthentication yes
  PreferredAuthentications gssapi-with-mic, publickey, password
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	hosts, err := ParseSSHConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("got %d hosts", len(hosts))
	}
	if !hosts[0].GSSAPIAuthentication {
		t.Fatal("expected GSSAPIAuthentication")
	}
	want := []string{"gssapi-with-mic", "publickey", "password"}
	if !reflect.DeepEqual(hosts[0].PreferredAuthentications, want) {
		t.Fatalf("got %#v want %#v", hosts[0].PreferredAuthentications, want)
	}
}
