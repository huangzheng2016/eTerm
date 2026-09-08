package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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
	aiProvidersSettingKey = "ai_providers"
	aiActiveSettingKey    = "ai_active"
)

type aiBridge struct {
	store *ai.Store
	db    *gorm.DB
	mk    *security.MasterKeyManager
	exec  ai.Executor

	mu             sync.Mutex
	agent          aiAgent
	agentKey       string
	cancel         context.CancelFunc
	running        bool
	runGen         int
	pendingHistory []byte
	cron           *ai.CronScheduler
	cronSession    string
	fireCh         chan<- aiToolRequest
}

type aiAgent interface {
	Run(ctx context.Context, input string) <-chan ai.Event
	Clear()
	ExportHistory(capBytes int) ([]byte, error)
	ImportHistory(data []byte) error
	UndoLastTurn()
	Enqueue(text string)
	ClearQueue()
	TaskSnapshots() []ai.TaskSnapshot
	CancelTask(id string) bool
}

func newAIBridge(database *gorm.DB, mk *security.MasterKeyManager, exec ai.Executor) *aiBridge {
	_ = database.AutoMigrate(&aiSession{}, &aiCronJob{})
	b := &aiBridge{store: loadAIStore(database, mk), db: database, mk: mk, exec: exec}
	if e, ok := exec.(*aiExecutor); ok {
		b.fireCh = e.reqCh
	}
	b.cron = ai.NewCronScheduler(b, b.deliverCron)
	return b
}

func (b *aiBridge) deliverCron(text string) {
	if b.fireCh == nil {
		return
	}
	b.fireCh <- aiToolRequest{op: aiToolCronFire, arg: text}
}

func (b *aiBridge) setCronSession(id string) {
	if b.cron == nil {
		return
	}
	b.mu.Lock()
	changed := b.cronSession != id
	b.cronSession = id
	b.mu.Unlock()
	if changed {
		b.cron.SetSession(id)
	}
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
	hasDaemons := false
	if e, ok := b.exec.(*aiExecutor); ok {
		hasDaemons = e.hasDaemons()
	}
	key := p.Name + "\x00" + model + "\x00" + strconv.FormatBool(hasDaemons)
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
		Daemons:        hasDaemons,
		Cron:           b.cron,
	})
	if err != nil {
		return nil, err
	}
	if b.pendingHistory != nil {
		_ = agent.ImportHistory(b.pendingHistory)
		b.pendingHistory = nil
	} else if b.agent != nil {
		if data, err := b.agent.ExportHistory(0); err == nil && len(data) > 0 {
			_ = agent.ImportHistory(data)
		}
	}
	if old, ok := b.agent.(interface{ Close() }); ok {
		old.Close()
	}
	b.agent = agent
	b.agentKey = key
	return agent, nil
}

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

func (b *aiBridge) ClearQueue() {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if agent != nil {
		agent.ClearQueue()
	}
}

func (b *aiBridge) DequeueLast() (string, bool) {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if d, ok := agent.(interface{ DequeueLast() (string, bool) }); ok {
		return d.DequeueLast()
	}
	return "", false
}

func (b *aiBridge) Compact(ctx context.Context) (aiview.CompactStats, error) {
	p, model, maxCtx, err := b.store.Resolve()
	if err != nil {
		return aiview.CompactStats{}, err
	}
	agent, err := b.agentFor(p, model, maxCtx)
	if err != nil {
		return aiview.CompactStats{}, err
	}
	c, ok := agent.(interface {
		Compact(context.Context) (ai.CompactStats, error)
	})
	if !ok {
		return aiview.CompactStats{}, errors.New("compact not supported by this agent")
	}
	stats, err := c.Compact(ctx)
	if err != nil {
		return aiview.CompactStats{}, err
	}
	return aiview.CompactStats{
		MessagesBefore: stats.MessagesBefore,
		MessagesAfter:  stats.MessagesAfter,
		TokensBefore:   stats.TokensBefore,
		TokensAfter:    stats.TokensAfter,
	}, nil
}

func (b *aiBridge) Tasks() []aiview.TaskEntry {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if agent == nil {
		return nil
	}
	snaps := agent.TaskSnapshots()
	out := make([]aiview.TaskEntry, 0, len(snaps))
	for _, s := range snaps {
		tail := make([]aiview.TaskActivity, 0, len(s.Tail))
		for _, a := range s.Tail {
			tail = append(tail, aiview.TaskActivity{Kind: a.Kind, Text: a.Text})
		}
		out = append(out, aiview.TaskEntry{
			ID:            s.ID,
			Task:          s.Task,
			Status:        string(s.Status),
			StartedSecAgo: s.StartedSecAgo,
			Tail:          tail,
		})
	}
	return out
}

func (b *aiBridge) CancelTask(id string) {
	b.mu.Lock()
	agent := b.agent
	b.mu.Unlock()
	if agent != nil {
		agent.CancelTask(id)
	}
}

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
		return aiview.AgentEvent{Kind: aiview.EventToolCallStart, Text: toolCallLabel(ev.ToolName, ev.ToolArgs), ToolName: ev.ToolName, ToolArgs: ev.ToolArgs}, true
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

func (b *aiBridge) Active() string {
	for _, m := range b.store.Models {
		if m.Alias == b.store.ActiveModel && m.Alias != "" {
			return m.Alias
		}
	}
	return b.store.ActiveProvider
}

func (b *aiBridge) Switch(provider, model string) {
	if b.store.ActiveProvider == provider && b.store.ActiveModel == model {
		return
	}
	if err := b.store.SetActive(provider, model); err != nil {
		return
	}
	b.CancelRun()
	b.persistActive()
}

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

func (a *App) updateAIView(msg tea.Msg) tea.Cmd {
	updated, cmd := a.aiView.Update(msg)
	if av, ok := updated.(*aiview.Model); ok {
		a.aiView = av
	}
	return cmd
}

func aiSkipForward(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return true
	}
	return false
}

func (a App) withAIStatusHint(hint string) string {
	if a.aiView == nil || a.aiVisible || !a.aiView.Running() {
		return hint
	}
	return "ai running (" + helpLabel(a.kbConfig.AIOverlay) + ") · " + hint
}
