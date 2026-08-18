// Package scheduler runs the poll-pull collector, the alert evaluator and the
// lease reaper on tickers. Each task retries with exponential backoff and, once
// its attempts are exhausted, writes a permanent failure record so an operator
// can re-inject the work. The scheduler stops gracefully on context cancellation.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"termduty/internal/config"
	"termduty/internal/domain"
	"termduty/internal/orchestration"
	"termduty/internal/store"
)

// TaskHealth is a snapshot of one background task used by the readiness check.
type TaskHealth struct {
	Name         string
	Failures     int
	LastAttempt  time.Time
	NextAttempt  time.Time
	LastRun      time.Time
	LastRunCount int
	LastError    string
	DeadLettered int
	Running      bool
}

// Scheduler owns the background tasks and their lifecycle.
type Scheduler struct {
	svc     *orchestration.Services
	cfg     config.Config
	clock   domain.Clock
	log     *slog.Logger
	mu      sync.Mutex
	tasks   []*task
	runWG   sync.WaitGroup
	stop    context.CancelFunc
	started bool
}

// New creates a scheduler wired to the service graph.
func New(svc *orchestration.Services, cfg config.Config, clock domain.Clock, log *slog.Logger) *Scheduler {
	s := &Scheduler{svc: svc, cfg: cfg, clock: clock, log: log}
	s.tasks = []*task{
		newTask("collector", cfg.IngestInterval, svc.Policy(), s.tickCollector),
		newTask("evaluator", cfg.EvalInterval, svc.Policy(), s.tickEvaluator),
		newTask("reaper", cfg.LeaseInterval, svc.Policy(), s.tickReaper),
	}
	return s
}

func newTask(name string, interval time.Duration, policy orchestration.RetryPolicy, tick func(context.Context) (int, error)) *task {
	return &task{name: name, interval: interval, policy: policy, tick: tick}
}

type task struct {
	name     string
	interval time.Duration
	policy   orchestration.RetryPolicy
	tick     func(context.Context) (int, error)

	failures     int
	lastAttempt  time.Time
	nextAttempt  time.Time
	lastRun      time.Time
	lastRunCount int
	lastError    string
	deadLettered int
}

// Start launches the task goroutines under the given parent context.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.stop = cancel
	s.started = true
	s.mu.Unlock()
	for _, t := range s.tasks {
		t := t
		s.runWG.Add(1)
		go s.runTask(runCtx, t)
	}
	return nil
}

// Stop cancels the tasks and waits for them to drain or the timeout to elapse.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.stop
	s.started = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.runWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Started reports whether the scheduler has been launched.
func (s *Scheduler) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// Health returns a snapshot of every task for the readiness probe.
func (s *Scheduler) Health() []TaskHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TaskHealth, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, TaskHealth{
			Name: t.name, Failures: t.failures, LastAttempt: t.lastAttempt,
			NextAttempt: t.nextAttempt, LastRun: t.lastRun, LastRunCount: t.lastRunCount,
			LastError: t.lastError, DeadLettered: t.deadLettered, Running: s.started,
		})
	}
	return out
}

func (s *Scheduler) runTask(ctx context.Context, t *task) {
	defer s.runWG.Done()
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Skip ticks that fall inside a retry backoff window so transient
			// failures throttle without abandoning the steady ticker beat.
			if s.inBackoff(t) {
				continue
			}
			s.runOnce(ctx, t)
		}
	}
}

// inBackoff reports whether a task should skip its tick because a previous
// attempt failed and the exponential backoff window has not elapsed yet.
func (s *Scheduler) inBackoff(t *task) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return t.failures > 0 && s.clock.Now().Before(t.nextAttempt)
}

// RunOnce runs a named task once synchronously. It is the testable entry point:
// tests drive a single iteration without real timers or network waits.
func (s *Scheduler) RunOnce(ctx context.Context, name string) error {
	t := s.find(name)
	if t == nil {
		return nil
	}
	s.runOnce(ctx, t)
	return nil
}

func (s *Scheduler) find(name string) *task {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.name == name {
			return t
		}
	}
	return nil
}

func (s *Scheduler) runOnce(ctx context.Context, t *task) {
	now := s.clock.Now()
	t.lastAttempt = now
	count, err := t.tick(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		t.failures++
		t.lastError = err.Error()
		t.lastRun = s.clock.Now()
		if t.failures >= t.policy.MaxAttempts {
			s.recordDeadLetter(ctx, t, err)
			t.failures = 0
			t.deadLettered++
		} else {
			t.nextAttempt = s.clock.Now().Add(t.policy.NextBackoff(t.failures))
		}
		return
	}
	t.failures = 0
	t.lastError = ""
	t.lastRun = s.clock.Now()
	t.lastRunCount = count
	t.nextAttempt = t.lastRun.Add(t.interval)
}

func (s *Scheduler) recordDeadLetter(ctx context.Context, t *task, err error) {
	fail := store.PermanentFailure{
		TaskType:  "scheduler." + t.name,
		LastError: err.Error(),
		Attempts:  t.policy.MaxAttempts,
		Status:    "dead",
		FailedAt:  s.clock.Now(),
	}
	if recordErr := s.svc.Store.Failures().Record(ctx, fail); recordErr != nil && s.log != nil {
		s.log.Warn("scheduler dead-letter record failed", "task", t.name, "err", recordErr)
	}
}

func (s *Scheduler) tickCollector(ctx context.Context) (int, error) {
	summary, err := s.svc.Ingest.ProcessBatch(ctx, s.clock.Now(), s.cfg.LeaseTTL, s.cfg.IngestBatchSize)
	if err != nil {
		return 0, err
	}
	return summary.Processed, nil
}

func (s *Scheduler) tickEvaluator(ctx context.Context) (int, error) {
	summary, err := s.svc.Alerts.Evaluate(ctx, s.clock.Now())
	if err != nil {
		return 0, err
	}
	return summary.Created, nil
}

func (s *Scheduler) tickReaper(ctx context.Context) (int, error) {
	n, err := s.svc.Ingest.Reclaim(ctx, s.clock.Now())
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// SetTick replaces the tick function of a named task. It is intended for tests
// that need to drive a task into the retry and dead-letter path deterministically
// without depending on real timers.
func (s *Scheduler) SetTick(name string, fn func(context.Context) (int, error)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.name == name {
			t.tick = fn
			return true
		}
	}
	return false
}
