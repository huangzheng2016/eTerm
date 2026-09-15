package voice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanTerminalContextLines(t *testing.T) {
	lines := []string{
		"┌─────────────┐",
		"│ NAME │ AGE  │",
		"├──────┼──────┤",
		"│ pod  │  3d  │",
		"└──────┴──────┘",
		"kubectl   get   pods",
		"kubectl get pods",
		"docker ps -a",
		"",
		"   ",
		"$ ▶►",
		"部署完成 2024",
	}
	got := CleanTerminalContextLines(lines, DefaultContextMaxLines)
	want := []string{"NAME AGE", "pod 3d", "kubectl get pods", "docker ps -a", "部署完成 2024"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCleanTerminalContextLinesKeepsLastN(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, strings.Repeat("x", i+1))
	}
	got := CleanTerminalContextLines(lines, 5)
	if len(got) != 5 || got[0] != strings.Repeat("x", 26) {
		t.Fatalf("got %v", got)
	}
}

func TestCleanTerminalContextLinesAdjacentDedupe(t *testing.T) {
	lines := []string{"alpha", "beta", "beta", "beta", "gamma", "alpha"}
	got := CleanTerminalContextLines(lines, 10)
	want := "alpha|beta|gamma|alpha"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestBuildDialogContextFormat(t *testing.T) {
	got := BuildDialogContext([]ContextTurn{
		{Speaker: "user", Text: "how do I list pods"},
		{Speaker: "bot", Text: "kubectl get pods"},
	}, 800)
	var v struct {
		Hotwords    []any         `json:"hotwords"`
		ContextType string        `json:"context_type"`
		ContextData []ContextTurn `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	if v.ContextType != "dialog_ctx" {
		t.Fatalf("context_type = %q", v.ContextType)
	}
	if v.Hotwords == nil || len(v.Hotwords) != 0 {
		t.Fatalf("hotwords = %v", v.Hotwords)
	}
	if len(v.ContextData) != 2 || v.ContextData[0].Speaker != "user" || v.ContextData[1].Speaker != "bot" {
		t.Fatalf("context_data = %+v", v.ContextData)
	}
}

func TestBuildDialogContextEmpty(t *testing.T) {
	if got := BuildDialogContext(nil, 800); got != "" {
		t.Fatalf("nil turns = %q", got)
	}
}

func TestBuildDialogContextDropsOldestOverBudget(t *testing.T) {
	turns := []ContextTurn{
		{Speaker: "user", Text: strings.Repeat("alpha ", 50)},
		{Speaker: "bot", Text: strings.Repeat("beta ", 50)},
		{Speaker: "user", Text: "kubectl get pods"},
	}
	got := BuildDialogContext(turns, 100)
	var v struct {
		ContextData []ContextTurn `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.ContextData) == 0 || v.ContextData[0].Text == turns[0].Text {
		t.Fatalf("oldest turn not dropped: %s", got)
	}
	if estimateContextTokens(got) > 100 {
		t.Fatalf("still over budget: %d", estimateContextTokens(got))
	}
}

func TestBuildDialogContextTruncatesSingleOversizedTurn(t *testing.T) {
	got := BuildDialogContext([]ContextTurn{{Speaker: "user", Text: strings.Repeat("abcd", 1000)}}, 100)
	var v struct {
		ContextData []ContextTurn `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.ContextData) != 1 || v.ContextData[0].Text == "" {
		t.Fatalf("single turn lost: %s", got)
	}
	if estimateContextTokens(got) > 100 {
		t.Fatalf("still over budget: %d", estimateContextTokens(got))
	}
}

func TestEstimateContextTokens(t *testing.T) {
	if got := estimateContextTokens("hello world"); got != 3 {
		t.Fatalf("ascii = %d", got)
	}
	if got := estimateContextTokens("你好世界"); got != 4 {
		t.Fatalf("cjk = %d", got)
	}
}
