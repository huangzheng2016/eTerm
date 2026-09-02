package ai

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// cronTestStore is a map-backed CronStore.
type cronTestStore struct {
	mu   sync.Mutex
	jobs map[string]CronJob
}

func newCronTestStore() *cronTestStore {
	return &cronTestStore{jobs: map[string]CronJob{}}
}

func (s *cronTestStore) LoadCronJobs(sessionID string) ([]CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []CronJob
	for _, j := range s.jobs {
		if j.SessionID == sessionID {
			out = append(out, j)
		}
	}
	return out, nil
}

func (s *cronTestStore) UpsertCronJob(job CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
	return nil
}

func (s *cronTestStore) DeleteCronJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	return nil
}

func (s *cronTestStore) DeleteSessionCronJobs(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.jobs {
		if j.SessionID == sessionID {
			delete(s.jobs, id)
		}
	}
	return nil
}

func (s *cronTestStore) MoveCronJobs(from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.jobs {
		if j.SessionID == from {
			j.SessionID = to
			s.jobs[id] = j
		}
	}
	return nil
}

func (s *cronTestStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

// cronTestScheduler builds a scheduler without the ticker goroutine: tests
// drive fireDue directly against the fake clock.
func cronTestScheduler(store CronStore, now time.Time, fires *[]string) *CronScheduler {
	return &CronScheduler{
		jobs:    map[string]*CronJob{},
		store:   store,
		deliver: func(text string) { *fires = append(*fires, text) },
		now:     func() time.Time { return now },
	}
}

func TestCronOneShotFiresOnceAndDeletes(t *testing.T) {
	store := newCronTestStore()
	now := time.Now()
	var fires []string
	s := cronTestScheduler(store, now, &fires)
	s.session = "sess"

	job, err := s.Create("check the build", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if store.count() != 1 {
		t.Fatal("job not persisted")
	}

	s.fireDue() // not yet due
	if len(fires) != 0 {
		t.Fatalf("fired early: %v", fires)
	}

	s.now = func() time.Time { return now.Add(6 * time.Minute) }
	s.fireDue()
	if len(fires) != 1 || !strings.Contains(fires[0], "check the build") || !strings.Contains(fires[0], job.ID) {
		t.Fatalf("fires: %v", fires)
	}
	if !strings.Contains(fires[0], "scheduled wake") {
		t.Fatalf("wake marker missing: %q", fires[0])
	}
	if n := len(s.List()); n != 0 || store.count() != 0 {
		t.Fatalf("one-shot not auto-deleted: mem %d, store %d", n, store.count())
	}

	s.fireDue()
	if len(fires) != 1 {
		t.Fatalf("one-shot fired twice: %v", fires)
	}
}

func TestCronRecurringCoalescesMissedFires(t *testing.T) {
	store := newCronTestStore()
	now := time.Now()
	var fires []string
	s := cronTestScheduler(store, now, &fires)
	s.session = "sess"

	if _, err := s.Create("report tab 1", 0, 5); err != nil {
		t.Fatal(err)
	}
	// 12 minutes past the first ideal fire: 3 ideal fires collapse into one.
	s.now = func() time.Time { return now.Add(17 * time.Minute) }
	s.fireDue()
	if len(fires) != 1 {
		t.Fatalf("missed fires not coalesced into one delivery: %v", fires)
	}
	if !strings.Contains(fires[0], "3 fires coalesced into one") {
		t.Fatalf("coalesce count missing: %q", fires[0])
	}
	jobs := s.List()
	if len(jobs) != 1 {
		t.Fatalf("recurring job gone: %v", jobs)
	}
	if want := now.Add(22 * time.Minute); !jobs[0].NextFireAt.Equal(want) {
		t.Fatalf("next fire = %v, want %v", jobs[0].NextFireAt, want)
	}
	stored, _ := store.LoadCronJobs("sess")
	if len(stored) != 1 || !stored[0].NextFireAt.Equal(now.Add(22*time.Minute)) {
		t.Fatalf("reschedule not persisted: %+v", stored)
	}

	// On-time fire reports no coalescing.
	s.now = func() time.Time { return now.Add(23 * time.Minute) }
	s.fireDue()
	if len(fires) != 2 || strings.Contains(fires[1], "coalesced") {
		t.Fatalf("fires: %v", fires)
	}
}

func TestCronCreateValidation(t *testing.T) {
	var fires []string
	s := cronTestScheduler(nil, time.Now(), &fires)
	if _, err := s.Create("", 5, 0); err == nil {
		t.Fatal("empty prompt accepted")
	}
	if _, err := s.Create("x", 0, 0); err == nil {
		t.Fatal("neither delay nor interval accepted")
	}
	if _, err := s.Create("x", 5, 5); err == nil {
		t.Fatal("both delay and interval accepted")
	}
	if _, err := s.Create("x", 0, 0); err == nil {
		t.Fatal("zero minutes accepted")
	}
	for i := 0; i < maxCronJobs; i++ {
		if _, err := s.Create("x", 5, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create("x", 5, 0); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("cap not enforced: %v", err)
	}
}

func TestCronPersistenceRoundTrip(t *testing.T) {
	store := newCronTestStore()
	now := time.Now()
	var fires []string
	s1 := cronTestScheduler(store, now, &fires)
	s1.session = "sess"
	if _, err := s1.Create("one", 5, 0); err != nil {
		t.Fatal(err)
	}
	// Distinct timestamps keep the List order deterministic (CreatedAt sort).
	s1.now = func() time.Time { return now.Add(time.Minute) }
	if _, err := s1.Create("two", 0, 10); err != nil {
		t.Fatal(err)
	}

	// A fresh scheduler on the same store sees the jobs after SetSession.
	s2 := cronTestScheduler(store, now, &fires)
	if n := len(s2.List()); n != 0 {
		t.Fatalf("jobs leaked across sessions: %d", n)
	}
	s2.SetSession("sess")
	jobs := s2.List()
	if len(jobs) != 2 || jobs[0].Prompt != "one" || jobs[1].Prompt != "two" {
		t.Fatalf("loaded jobs: %+v", jobs)
	}

	// Jobs of another session stay out.
	s2.SetSession("other")
	if n := len(s2.List()); n != 0 {
		t.Fatalf("wrong session jobs loaded: %d", n)
	}
}

// Jobs sharing a CreatedAt sort deterministically by id.
func TestCronListSameTimestampTieBreak(t *testing.T) {
	var fires []string
	s := cronTestScheduler(nil, time.Now(), &fires)
	s.session = "sess"
	first, err := s.Create("a", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create("b", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	jobs := s.List()
	want := []string{first.ID, second.ID}
	slices.Sort(want)
	if len(jobs) != 2 || jobs[0].ID != want[0] || jobs[1].ID != want[1] {
		t.Fatalf("same-timestamp order not id-sorted: %+v", jobs)
	}
}

// Jobs created before the first save (session "") are re-homed to the first
// real session id, in store and in memory.
func TestCronSetSessionAdoptsUnsavedJobs(t *testing.T) {
	store := newCronTestStore()
	var fires []string
	s := cronTestScheduler(store, time.Now(), &fires)
	if _, err := s.Create("check back", 5, 0); err != nil {
		t.Fatal(err)
	}
	s.SetSession("s1")
	jobs := s.List()
	if len(jobs) != 1 || jobs[0].SessionID != "s1" {
		t.Fatalf("job not adopted: %+v", jobs)
	}
	stored, _ := store.LoadCronJobs("s1")
	if len(stored) != 1 {
		t.Fatalf("adoption not persisted: %+v", stored)
	}
	// A later switch does not re-adopt.
	s.SetSession("s2")
	if n := len(s.List()); n != 0 {
		t.Fatalf("s2 must be empty: %d", n)
	}
	if stored, _ := store.LoadCronJobs("s1"); len(stored) != 1 {
		t.Fatal("s1 jobs moved again")
	}
}

// AbandonSession wipes the active session's jobs from memory and the store,
// including jobs of the unsaved session (""); other sessions are untouched.
func TestCronAbandonSessionWipesJobs(t *testing.T) {
	store := newCronTestStore()
	var fires []string
	s := cronTestScheduler(store, time.Now(), &fires)

	// Jobs created before the first save (session "").
	if _, err := s.Create("pre-save", 5, 0); err != nil {
		t.Fatal(err)
	}
	s.AbandonSession()
	if n := len(s.List()); n != 0 || store.count() != 0 {
		t.Fatalf("unsaved jobs not wiped: mem %d, store %d", n, store.count())
	}

	// Jobs of a saved session; a second session's jobs must survive.
	s.SetSession("s1")
	if _, err := s.Create("s1 job", 5, 0); err != nil {
		t.Fatal(err)
	}
	s.SetSession("s2")
	if _, err := s.Create("s2 job", 5, 0); err != nil {
		t.Fatal(err)
	}
	s.AbandonSession()
	if n := len(s.List()); n != 0 {
		t.Fatalf("jobs not wiped from memory: %d", n)
	}
	if stored, _ := store.LoadCronJobs("s2"); len(stored) != 0 {
		t.Fatalf("s2 jobs not wiped from store: %+v", stored)
	}
	if stored, _ := store.LoadCronJobs("s1"); len(stored) != 1 {
		t.Fatalf("other session jobs wiped: %+v", stored)
	}

	// The wiped session stays empty when re-entered.
	s.SetSession("s2")
	if n := len(s.List()); n != 0 {
		t.Fatalf("wiped jobs came back: %d", n)
	}
}

func TestCronDelete(t *testing.T) {
	store := newCronTestStore()
	var fires []string
	s := cronTestScheduler(store, time.Now(), &fires)
	s.session = "sess"
	job, err := s.Create("x", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Delete("missing") {
		t.Fatal("unknown id deleted")
	}
	if !s.Delete(job.ID) {
		t.Fatal("delete failed")
	}
	if store.count() != 0 {
		t.Fatal("delete not persisted")
	}
}

func TestBuildToolsWiresCron(t *testing.T) {
	var fires []string
	s := cronTestScheduler(nil, time.Now(), &fires)
	tools, err := BuildTools(fakeExecutor{}, s, false)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]tool.BaseTool{}
	for _, bt := range tools {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byName[info.Name] = bt
	}
	for _, name := range []string{"cron_create", "cron_list", "cron_delete"} {
		if byName[name] == nil {
			t.Fatalf("%s missing from BuildTools", name)
		}
	}
	plain, err := BuildTools(fakeExecutor{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, bt := range plain {
		info, _ := bt.Info(context.Background())
		if strings.HasPrefix(info.Name, "cron_") {
			t.Fatalf("%s present without a scheduler", info.Name)
		}
	}

	ctx := context.Background()
	type invokable interface {
		InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error)
	}
	create := byName["cron_create"].(invokable)
	out, err := create.InvokableRun(ctx, `{"prompt":"check the build","interval_minutes":5}`)
	if err != nil || !strings.Contains(out, `"kind":"interval"`) {
		t.Fatalf("cron_create: %q %v", out, err)
	}
	list := byName["cron_list"].(invokable)
	out, err = list.InvokableRun(ctx, `{}`)
	if err != nil || !strings.Contains(out, "check the build") {
		t.Fatalf("cron_list: %q %v", out, err)
	}
	job := s.List()[0]
	del := byName["cron_delete"].(invokable)
	out, err = del.InvokableRun(ctx, `{"id":"`+job.ID+`"}`)
	if err != nil || !strings.Contains(out, `"deleted":true`) {
		t.Fatalf("cron_delete: %q %v", out, err)
	}
	out, err = del.InvokableRun(ctx, `{"id":"`+job.ID+`"}`)
	if err != nil || !strings.Contains(out, "no cron job with id") {
		t.Fatalf("cron_delete twice: %q %v", out, err)
	}
}
