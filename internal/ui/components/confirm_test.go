package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestConfirmMouseClickYes(t *testing.T) {
	c := NewConfirm("Delete", "Delete item?").Show()

	c, _ = c.Update(tea.MouseClickMsg(tea.Mouse{X: 5, Y: 6, Button: tea.MouseLeft}))

	if c.IsActive() {
		t.Fatal("expected confirm to close")
	}
	if !c.Result() {
		t.Fatal("expected yes result")
	}
}

func TestConfirmMouseClickNo(t *testing.T) {
	c := NewConfirm("Delete", "Delete item?").Show()

	c, _ = c.Update(tea.MouseClickMsg(tea.Mouse{X: 16, Y: 6, Button: tea.MouseLeft}))

	if c.IsActive() {
		t.Fatal("expected confirm to close")
	}
	if c.Result() {
		t.Fatal("expected no result")
	}
}
