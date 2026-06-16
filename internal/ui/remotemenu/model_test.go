package remotemenu

import (
	"strings"
	"testing"

	"github.com/huangzheng2016/eTerm/internal/types"
)

func TestRemoteMenuDoesNotUseTabPrefixesOrListTags(t *testing.T) {
	m := New(types.RemotePeer{Name: "peer"}, []types.RemoteHost{{Alias: "prod", Hostname: "prod.example", Username: "root", Port: 22}})

	view := m.View()

	for _, want := range []string{"peer", "LocalShell", "prod"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	for _, bad := range []string{"[Default]", "[Daemon]", "[L]", "[S]", "[R]"} {
		if strings.Contains(view, bad) {
			t.Fatalf("view contains %s:\n%s", bad, view)
		}
	}
}
