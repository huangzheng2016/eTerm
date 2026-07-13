package fwdview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEscapeDoesNotCloseForwardList(t *testing.T) {
	_, cmd := (Model{}).Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if cmd != nil {
		t.Fatal("escape closed the forward list")
	}
}
