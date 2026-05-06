package version

import "testing"

func TestParseChecksumsFile(t *testing.T) {
	input := []byte(`
# comment
abcd1234deadbeef0000000000000000000000000000000000000000001234  eterm_linux_amd64.tar.gz
`)

	m := ParseChecksumsFile(input)
	got := m["eterm_linux_amd64.tar.gz"]
	if got != "abcd1234deadbeef0000000000000000000000000000000000000000001234" {
		t.Fatalf("unexpected: %q", got)
	}
}
