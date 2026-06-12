package main

import "testing"

func TestSplitUpgradeCommand(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		wantUpgrade bool
		wantArgs    []string
	}{
		{
			name:        "upgrade command",
			args:        []string{"upgrade"},
			wantUpgrade: true,
			wantArgs:    []string{},
		},
		{
			name:        "upgrade keeps remaining args",
			args:        []string{"upgrade", "root@example.com"},
			wantUpgrade: true,
			wantArgs:    []string{"root@example.com"},
		},
		{
			name:        "host named upgrade after flag is not command",
			args:        []string{"-p", "2222", "upgrade"},
			wantUpgrade: false,
			wantArgs:    []string{"-p", "2222", "upgrade"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotUpgrade, gotArgs := splitUpgradeCommand(tc.args)
			if gotUpgrade != tc.wantUpgrade {
				t.Fatalf("upgrade=%v want %v", gotUpgrade, tc.wantUpgrade)
			}
			if len(gotArgs) != len(tc.wantArgs) {
				t.Fatalf("args=%v want %v", gotArgs, tc.wantArgs)
			}
			for i := range gotArgs {
				if gotArgs[i] != tc.wantArgs[i] {
					t.Fatalf("args=%v want %v", gotArgs, tc.wantArgs)
				}
			}
		})
	}
}
