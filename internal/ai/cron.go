package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const (
	maxCronJobs    = 20
	minCronMinutes = 1
)

// cronTickInterval bounds fire latency; var so tests can shrink it.
var cronTickInterval = time.Second

// CronJob is one scheduled wake. IntervalMinutes > 0 means recurring; 0 is a
// one-shot that auto-deletes after firing.
type CronJob struct {
	ID              string
	SessionID       string
	Prompt          string
	IntervalMinutes int
	NextFireAt      time.Time
	CreatedAt       time.Time
}

// CronStore persists cron jobs (implemented by the app bridge via gorm).
type CronStore interface {
	LoadCronJobs(sessionID string) ([]CronJob, error)
	UpsertCronJob(job CronJob) error
	DeleteCronJob(id string) error
	// MoveCronJobs re-homes jobs from one session to another (first save of
	// a previously unsaved conversation).
	MoveCronJobs(fromSession, toSession string) error
}

// CronScheduler fires due jobs of the active session, delivering each fire as
// composed wake text. Every change is persisted, so jobs survive restarts;
// jobs due while their session was inactive fire once (missed fires
// coalesced) after the session loads again. now is fixed at construction
// (tests build the struct directly); the ticker goroutine lives for the
// process (the bridge is created once).
type CronScheduler struct {
	mu      sync.Mutex
	jobs    map[string]*CronJob
	store   CronStore
	deliver func(text string)
	session string
	now     func() time.Time
}

func NewCronScheduler(store CronStore, deliver func(text string)) *CronScheduler {
	s := &CronScheduler{
		jobs:    map[string]*CronJob{},
		store:   store,
		deliver: deliver,
		now:     time.Now,
	}
	go s.loop()
	return s
}

func (s *CronScheduler) loop() {
	tick := time.NewTicker(cronTickInterval)
	defer tick.Stop()
	for range tick.C {
		s.fireDue()
	}
}

// SetSession switches the active session: the previous session's jobs stay
// persisted but stop firing; the new session's jobs load from the store and
// overdue ones fire on the next tick. Jobs of the unnamed pre-save session
// ("") are re-homed to the first real id; the move and the reload happen
// under the same lock so a concurrent Create lands in the right session.
func (s *CronScheduler) SetSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == "" && id != "" && s.store != nil {
		_ = s.store.MoveCronJobs("", id)
	}
	s.session = id
	s.jobs = map[string]*CronJob{}
	if s.store == nil || id == "" {
		return
	}
	stored, err := s.store.LoadCronJobs(id)
	if err != nil {
		return
	}
	for i := range stored {
		job := stored[i]
		s.jobs[job.ID] = &job
	}
}

func (s *CronScheduler) Create(prompt string, delayMin, intervalMin int) (CronJob, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return CronJob{}, fmt.Errorf("prompt is required")
	}
	if (delayMin > 0) == (intervalMin > 0) {
		return CronJob{}, fmt.Errorf("give exactly one of delay_minutes (one-shot) or interval_minutes (recurring)")
	}
	minutes := delayMin
	if intervalMin > 0 {
		minutes = intervalMin
	}
	if minutes < minCronMinutes {
		return CronJob{}, fmt.Errorf("minimum is %d minute", minCronMinutes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) >= maxCronJobs {
		return CronJob{}, fmt.Errorf("cron job cap reached (max %d per session)", maxCronJobs)
	}
	now := s.now()
	job := CronJob{
		ID:              s.newID(),
		SessionID:       s.session,
		Prompt:          prompt,
		IntervalMinutes: intervalMin,
		NextFireAt:      now.Add(time.Duration(minutes) * time.Minute),
		CreatedAt:       now,
	}
	s.jobs[job.ID] = &job
	if s.store != nil {
		_ = s.store.UpsertCronJob(job)
	}
	return job, nil
}

func (s *CronScheduler) List() []CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, *job)
	}
	slices.SortFunc(out, func(a, b CronJob) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		// Same-timestamp jobs (fake clocks, fast successive creates) need a
		// stable tie-break: map iteration order is random.
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func (s *CronScheduler) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return false
	}
	delete(s.jobs, id)
	if s.store != nil {
		_ = s.store.DeleteCronJob(id)
	}
	return true
}

// newID: caller holds mu.
func (s *CronScheduler) newID() string {
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return fmt.Sprintf("cron-%d", time.Now().UnixNano())
		}
		id := hex.EncodeToString(b[:])
		if _, taken := s.jobs[id]; !taken {
			return id
		}
	}
}

func (s *CronScheduler) fireDue() {
	var fires []string
	now := s.now()
	s.mu.Lock()
	for _, job := range s.jobs {
		if job.NextFireAt.After(now) {
			continue
		}
		coalesced := 1
		if job.IntervalMinutes > 0 {
			interval := time.Duration(job.IntervalMinutes) * time.Minute
			coalesced += int(now.Sub(job.NextFireAt) / interval)
			job.NextFireAt = now.Add(interval)
			if s.store != nil {
				_ = s.store.UpsertCronJob(*job)
			}
		} else {
			delete(s.jobs, job.ID)
			if s.store != nil {
				_ = s.store.DeleteCronJob(job.ID)
			}
		}
		fires = append(fires, cronWakeText(*job, coalesced))
	}
	s.mu.Unlock()
	for _, text := range fires {
		s.deliver(text)
	}
}

// cronWakeText marks the fire as a scheduled wake so the agent does not
// mistake it for live user input.
func cronWakeText(job CronJob, coalesced int) string {
	sched := "one-shot"
	if job.IntervalMinutes > 0 {
		sched = fmt.Sprintf("every %dm", job.IntervalMinutes)
	}
	head := fmt.Sprintf("[cron job %s fired (%s", job.ID, sched)
	if coalesced > 1 {
		head += fmt.Sprintf(", %d fires coalesced into one", coalesced)
	}
	return head + ") - scheduled wake, not live user input]\n" + job.Prompt
}

type CronCreateInput struct {
	Prompt          string `json:"prompt" jsonschema_description:"Instruction delivered back to you as a user message when the job fires"`
	DelayMinutes    int    `json:"delay_minutes,omitempty" jsonschema_description:"One-shot: fire once after this many minutes (min 1), then auto-delete. Give this or interval_minutes, not both"`
	IntervalMinutes int    `json:"interval_minutes,omitempty" jsonschema_description:"Recurring: fire every this many minutes (min 1) until cancelled with cron_delete. Give this or delay_minutes, not both"`
}

type CronCreateOutput struct {
	ID         string `json:"id,omitempty"`
	Kind       string `json:"kind,omitempty"` // once | interval
	NextFireAt string `json:"next_fire_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

type CronListInput struct{}

type CronListJob struct {
	ID              string `json:"id"`
	Prompt          string `json:"prompt"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"` // 0 = one-shot
	NextFireInSec   int    `json:"next_fire_in_sec"`
}

type CronListOutput struct {
	Jobs []CronListJob `json:"jobs"`
}

type CronDeleteInput struct {
	ID string `json:"id" jsonschema_description:"Job id from cron_create or cron_list"`
}

type CronDeleteOutput struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

func buildCronTools(s *CronScheduler) ([]tool.BaseTool, error) {
	create, err := utils.InferTool("cron_create", "Schedule a prompt to wake you later in this conversation; the fire arrives as a user message. Use when the user asks you to watch or monitor something and report back (keep an eye on the build, check back in N minutes, tell me when it finishes), or to check on a long task running in a tab periodically. delay_minutes is a one-shot reminder (fires once, auto-deletes); interval_minutes is a recurring check (min 1); give exactly one. Jobs survive app restart (missed fires coalesce into one wake), are capped at 20 per session, and are cancelled with cron_delete. Prefer cron over sleep whenever the wait is more than a few minutes: sleep blocks the current run (max 600s) while cron wakes you without holding it", s.createTool)
	if err != nil {
		return nil, fmt.Errorf("build cron_create: %w", err)
	}
	list, err := utils.InferTool("cron_list", "List this session's scheduled cron jobs with their id, prompt, interval_minutes (0 = one-shot) and seconds until the next fire - both one-shot reminders and recurring watches created with cron_create", s.listTool)
	if err != nil {
		return nil, fmt.Errorf("build cron_list: %w", err)
	}
	del, err := utils.InferTool("cron_delete", "Cancel a scheduled cron job by id (from cron_create or cron_list). Recurring jobs stop firing; a pending one-shot is cancelled. Deletion is irreversible", s.deleteTool)
	if err != nil {
		return nil, fmt.Errorf("build cron_delete: %w", err)
	}
	return []tool.BaseTool{create, list, del}, nil
}

func (s *CronScheduler) createTool(ctx context.Context, in *CronCreateInput) (*CronCreateOutput, error) {
	job, err := s.Create(in.Prompt, in.DelayMinutes, in.IntervalMinutes)
	if err != nil {
		return &CronCreateOutput{Error: err.Error()}, nil
	}
	kind := "once"
	if job.IntervalMinutes > 0 {
		kind = "interval"
	}
	return &CronCreateOutput{ID: job.ID, Kind: kind, NextFireAt: job.NextFireAt.Format(time.RFC3339)}, nil
}

func (s *CronScheduler) listTool(ctx context.Context, in *CronListInput) (*CronListOutput, error) {
	now := s.now()
	out := &CronListOutput{}
	for _, job := range s.List() {
		out.Jobs = append(out.Jobs, CronListJob{
			ID:              job.ID,
			Prompt:          job.Prompt,
			IntervalMinutes: job.IntervalMinutes,
			NextFireInSec:   int(job.NextFireAt.Sub(now).Seconds()),
		})
	}
	return out, nil
}

func (s *CronScheduler) deleteTool(ctx context.Context, in *CronDeleteInput) (*CronDeleteOutput, error) {
	if !s.Delete(in.ID) {
		return &CronDeleteOutput{Error: "no cron job with id " + in.ID}, nil
	}
	return &CronDeleteOutput{Deleted: true}, nil
}
