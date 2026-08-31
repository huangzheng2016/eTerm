package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/huangzheng2016/eTerm/internal/ai"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"github.com/huangzheng2016/eTerm/internal/ui/aiview"

	tea "charm.land/bubbletea/v2"
	"gorm.io/gorm"
)

const (
	aiProvidersSettingKey = "ai_providers" // encrypted JSON []ai.Provider (user-added only)
	aiActiveSettingKey    = "ai_active"    // plain JSON {provider, model}
)

// aiBridge adapts ai.Agent + ai.Store to the aiview interfaces. It also
// persists user-added providers (encrypted) and the active selection.
type aiBridge struct {
	store *ai.Store
	db    *gorm.DB
	mk    *security.MasterKeyManager
	exec  ai.Executor

	mu       sync.Mutex
	agent    aiAgent
	agentKey string
	cancel   context.CancelFunc
	// running reports a run is in flight (guarded by mu, runGen defeats a
	// stale pump goroutine clearing a newer run's flag).
	running bool
	runGen  int
	// pendingHistory holds a resumed session's history until the first agent
	// exists (no runs yet in this process).
	pendingHistory []byte
}

// aiAgent is the part of ai.Agent the bridge uses; a seam for tests.
type aiAgent interface {
	Run(ctx context.Context, input string) <-chan ai.Event
	Clear()
	ExportHistory(capBytes int) ([]byte, error)
	ImportHistory(data []byte) error
	UndoLastTurn()
	Enqueue(text string)
	ClearQueue()
}

func newAIBridge(database *gorm.DB, mk *security.MasterKeyManager, exec ai.Executor) *aiBridge {
	_ = database.AutoMigrate(&aiSession{})
	return &aiBridge{store: loadAIStore(database, mk), db: database, mk: mk, exec: exec}
}

func loadAIStore(database *gorm.DB, mk *security.MasterKeyManager) *ai.Store {
	store := &ai.Store{}
	if enc, err := db.GetSetting(database, aiProvidersSettingKey); err == nil && enc != "" {
		if k := mk.GetKey(); k != nil {
			plain, err := security.Decrypt(enc, k.Bytes())
			k.Clear()
			if err == nil {
				_ = json.Unmarshal(plain, &store.Providers)
			}
		}
	}
	if v, err := db.GetSetting(database, aiActiveSettingKey); err == nil && v != "" {
		var act struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if json.Unmarshal([]byte(v), &act) == nil && act.Provider != "" {
			store.ActiveProvider = act.Provider
			store.ActiveModel = act.Model
		}
	}
	// User-added providers and the saved selection load first; kimi imports
	// only fill gaps (existing names win).
	if kimiCfg, err := ai.LoadKimiConfig(ai.KimiConfigPath()); err == nil {
		store.ImportKimi(kimiCfg)
	}
	return store
}

func (b *aiBridge) persistProviders() {
	var user []ai.Provider
	for _, p := range b.store.Providers {
		if p.Source != ai.SourceKimi {
			user = append(user, p)
		}
	}
	data, err := json.Marshal(user)
	if err != nil {
		return
	}
	k := b.mk.GetKey()
	if k == nil {
		return
	}
	defer k.Clear()
	enc, err := security.Encrypt(data, k.Bytes())
	if err != nil {
		return
	}
	_ = db.SetSetting(b.db, aiProvidersSettingKey, enc)
}

func (b *aiBridge) persistActive() {
	data, err := json.Marshal(map[string]string{
		"provider": b.store.ActiveProvider,
		"model":    b.store.ActiveModel,
	})
	if err != nil {
		return
	}
	_ = db.SetSetting(b.db, aiActiveSettingKey, string(data))
}

// Run implements aiview.AgentRunner.
func (b *aiBridge) Run(ctx context.Context, prompt string) (<-chan aiview.AgentEvent, error) {
	p, model, maxCtx, err := b.store.Resolve()
	if err != nil {
		return nil, err
	}
	agent, err := b.agentFor(p, model, maxCtx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.cancel = cancel
	b.runGen++
	b.running = true
	gen := b.runGen
	b.mu.Unlock()
	src := agent.Run(ctx, prompt)
	out := make(chan aiview.AgentEvent, 64)
	go func() {
		defer close(out)
		defer cancel()
		defer func() {
			b.mu.Lock()
			if b.runGen == gen {
				b.running = false
			}
			b.mu.Unlock()
		}()
		for ev := range src {
			ae, ok := aiEventToView(ev)
			if !ok {
				continue
			}
			select {
			case out <- ae:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (b *aiBridge) agentFor(p *ai.Provider, model string, maxCtx int) (aiAgent, error) {
	key := p.Name + "\x00" + model
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agent != nil && b.agentKey == key {
		return b.agent, nil
	}
	agent, err := ai.NewAgent(context.Background(), ai.Config{
		Provider:       p,
		Model:          model,
		MaxContextSize: maxCtx,
		Executor:       b.exec,
	})
	if err != nil {
		return nil, err
	}
	// Keep the conversation across the swap: a resumed session waiting for
	// the first agent, or the replaced agent's history on a provider/model
	// switch (the panel keeps its blocks, so the agent keeps its context).
	if b.pendingHistory != nil {
		_ = agent.ImportHistory(b.pendingHistory)
		b.pendingHistory = nil
	} else if b.agent != nil {
		if data, err := b.agent.ExportHistory(0); err == nil && len(data) > 0 {
			_ = agent.ImportHistory(data)
		}
	}
	// Close is not on the aiAgent seam; it cancels background tasks so a
	// replaced agent does not leak them.
	if old, ok := b.agent.(interface{ Close() }); ok {
		old.Close()
	}
	b.agent = agent
	b.agentKey = key
	return agent, nil
}

// Clear resets the agent conversation history (wired to the overlay's ctrl+l).
// Agent.Clear blocks on the run mutex for the rest of the turn, so it runs
// off the caller's goroutine: the overlay cancels the run in the same key
// handling, and the clear lands as soon as the turn unwinds.
func (b *aiBridge) Clear() {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if agent != nil {
		go agent.Clear()
	}
}

// Enqueue implements aiview.AgentRunner: input submitted mid-run is queued on
// the agent, which injects it at the next step boundary. It fails rather than
// dropping the message silently when there is no run to steer.
func (b *aiBridge) Enqueue(text string) error {
	p, model, maxCtx, err := b.store.Resolve()
	if err != nil {
		return err
	}
	agent, err := b.agentFor(p, model, maxCtx)
	if err != nil {
		return err
	}
	b.mu.Lock()
	running := b.running
	b.mu.Unlock()
	if !running {
		return errors.New("no run in progress")
	}
	agent.Enqueue(text)
	return nil
}

// ClearQueue implements aiview.AgentRunner.
func (b *aiBridge) ClearQueue() {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if agent != nil {
		agent.ClearQueue()
	}
}

// CancelRun aborts the in-flight run, if any (used on lock).
func (b *aiBridge) CancelRun() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running = false
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.agent != nil {
		b.agent.ClearQueue()
	}
}

// ContextUsage reports the active agent's context token usage (aiview title
// bar). Zero values when no agent exists yet; Usage is not on the aiAgent
// seam, hence the assertion.
func (b *aiBridge) ContextUsage() (used, max int) {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if u, ok := agent.(interface{ Usage() (int, int) }); ok {
		return u.Usage()
	}
	return 0, 0
}

func aiEventToView(ev ai.Event) (aiview.AgentEvent, bool) {
	switch ev.Type {
	case ai.EventTextDelta:
		return aiview.AgentEvent{Kind: aiview.EventTextDelta, Text: ev.Text}, true
	case ai.EventThinkingDelta:
		return aiview.AgentEvent{Kind: aiview.EventThinkingDelta, Text: ev.Text}, true
	case ai.EventToolCall:
		return aiview.AgentEvent{Kind: aiview.EventToolCallStart, Text: toolCallLabel(ev.ToolName, ev.ToolArgs)}, true
	case ai.EventToolResult:
		return aiview.AgentEvent{Kind: aiview.EventToolCallEnd, Text: ev.Text}, true
	case ai.EventDone:
		return aiview.AgentEvent{Kind: aiview.EventDone}, true
	case ai.EventSteer:
		return aiview.AgentEvent{Kind: aiview.EventSteer, Text: ev.Text}, true
	case ai.EventError:
		text := ""
		if ev.Err != nil {
			text = ev.Err.Error()
		}
		return aiview.AgentEvent{Kind: aiview.EventError, Text: text}, true
	}
	return aiview.AgentEvent{}, false
}

func toolCallLabel(name, args string) string {
	args = strings.Join(strings.Fields(args), " ")
	if r := []rune(args); len(r) > 120 {
		args = string(r[:120]) + "..."
	}
	if args == "" {
		return name
	}
	return name + " " + args
}

// Models implements aiview.ProviderStore: one entry per model alias, plus one
// per provider not covered by any alias (user-added).
func (b *aiBridge) Models() []aiview.ModelEntry {
	aliased := map[string]bool{}
	out := make([]aiview.ModelEntry, 0, len(b.store.Models)+len(b.store.Providers))
	for _, m := range b.store.Models {
		aliased[m.Provider] = true
		typ := ""
		if p := b.store.Get(m.Provider); p != nil {
			typ = p.Type
		}
		out = append(out, aiview.ModelEntry{Label: m.Alias, Provider: m.Provider, Model: m.Alias, Type: typ})
	}
	for _, p := range b.store.Providers {
		if aliased[p.Name] {
			continue
		}
		out = append(out, aiview.ModelEntry{Label: p.Name, Provider: p.Name, Model: p.DefaultModel, Type: p.Type})
	}
	return out
}

// Active implements aiview.ProviderStore: the active alias when ActiveModel
// names one, else the active provider name.
func (b *aiBridge) Active() string {
	for _, m := range b.store.Models {
		if m.Alias == b.store.ActiveModel && m.Alias != "" {
			return m.Alias
		}
	}
	return b.store.ActiveProvider
}

// Switch implements aiview.ProviderStore.
func (b *aiBridge) Switch(provider, model string) {
	if b.store.ActiveProvider == provider && b.store.ActiveModel == model {
		return
	}
	if err := b.store.SetActive(provider, model); err != nil {
		return
	}
	// A switch mid-run strands the old agent's turn on the old provider.
	b.CancelRun()
	b.persistActive()
}

// Add implements aiview.ProviderStore.
func (b *aiBridge) Add(pv aiview.Provider) {
	name := strings.TrimSpace(pv.Name)
	if name == "" {
		return
	}
	b.store.Upsert(ai.Provider{
		Name:         name,
		Type:         strings.ToLower(strings.TrimSpace(pv.Type)),
		APIKey:       pv.APIKey,
		BaseURL:      strings.TrimSpace(pv.BaseURL),
		DefaultModel: strings.TrimSpace(pv.Model),
	})
	b.persistProviders()
}

// ensureAI builds the AI overlay once per process (it persists across page
// switches and lock/unlock). Returns the tool-request pump command on first
// creation.
func (a App) ensureAI() (App, tea.Cmd) {
	if a.aiView != nil {
		return a, nil
	}
	exec := &aiExecutor{db: a.db, mk: a.masterKey, reqCh: a.aiToolCh, shared: a.aiShared}
	a.aiBridge = newAIBridge(a.db, a.masterKey, exec)
	a.aiView = aiview.New(a.aiBridge, a.aiBridge, a.aiBridge)
	if a.width > 0 && a.height > 0 {
		a.aiView.SetSize(a.width, a.height)
	}
	return a, waitAIToolRequest(a.aiToolCh)
}

func (a App) openAIOverlay() (App, tea.Cmd) {
	if a.aiView == nil {
		return a, nil
	}
	a.aiVisible = true
	return a, a.aiView.Init()
}

// updateAIView routes a message into the overlay model.
func (a *App) updateAIView(msg tea.Msg) tea.Cmd {
	updated, cmd := a.aiView.Update(msg)
	if av, ok := updated.(*aiview.Model); ok {
		a.aiView = av
	}
	return cmd
}

// aiSkipForward reports messages the blanket forward to the overlay must not
// carry: interactive input is delivered through the interception chain when
// the overlay is visible, and dropped for it when hidden.
func aiSkipForward(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return true
	}
	return false
}

// withAIStatusHint adds a small indicator while an agent run is active and
// the overlay is hidden.
func (a App) withAIStatusHint(hint string) string {
	if a.aiView == nil || a.aiVisible || !a.aiView.Running() {
		return hint
	}
	return "ai running (" + helpLabel(a.kbConfig.AIOverlay) + ") · " + hint
}
