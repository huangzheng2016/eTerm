package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestConfirmMouseClick(t *testing.T) {
	cases := []struct {
		name string
		x    int
		want bool
	}{
		{"yes", 5, true},
		{"no", 16, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewConfirm("Delete", "Delete item?").Show()

			c, _ = c.Update(tea.MouseClickMsg(tea.Mouse{X: tc.x, Y: 6, Button: tea.MouseLeft}))

			if c.IsActive() {
				t.Fatal("expected confirm to close")
			}
			if c.Result() != tc.want {
				t.Fatalf("result = %v want %v", c.Result(), tc.want)
			}
		})
	}
}
