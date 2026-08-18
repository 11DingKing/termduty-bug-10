package scheduler_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"termduty/internal/config"
	"termduty/internal/domain"
	"termduty/internal/orchestration"
	"termduty/internal/scheduler"
	"termduty/internal/store"
)

func newSchedulerSvc(t *testing.T) (*orchestration.Services, *domain.FakeClock, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	clock := domain.NewFakeClock(time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.New(context.Background(), filepath.Join(dir, "termduty.db"), filepath.Join(dir, "shards"), clock, log)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return orchestration.New(st, clock, log), clock, st
}

func defaultCfg(dir string) config.Config {
	c := config.Default(dir)
	c.IngestInterval = 10 * time.Millisecond
	c.EvalInterval = 10 * time.Millisecond
	c.LeaseInterval = 10 * time.Millisecond
	return c
}

// TestRetryPolicy_NextBackoffExponential verifies the delay grows per attempt.
func TestRetryPolicy_NextBackoffExponential(t *testing.T) {
	p := orchestration.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: func() float64 { return 0.5 }}
	d1 := p.NextBackoff(1)
	d2 := p.NextBackoff(2)
	d3 := p.NextBackoff(3)
	if !(d2 > d1 && d3 > d2) {
		t.Fatalf("backoff not increasing: %v %v %v", d1, d2, d3)
	}
	if d3 > p.MaxDelay {
		t.Fatalf("backoff exceeds max: %v", d3)
	}
}

// TestScheduler_RunOnce_DeadLettersAfterExhaustion drives a failing task past
// its retry limit and confirms a permanent failure is recorded.
func TestScheduler_RunOnce_DeadLettersAfterExhaustion(t *testing.T) {
	svc, clock, _ := newSchedulerSvc(t)
	sched := scheduler.New(svc, defaultCfg(t.TempDir()), clock, slog.New(slog.NewTextHandler(io.Discard, nil)))
	boom := errors.New("boom")
	sched.SetTick("collector", func(ctx context.Context) (int, error) { return 0, boom })
	for i := 0; i < svc.Policy().MaxAttempts; i++ {
		if err := sched.RunOnce(context.Background(), "collector"); err != nil {
			t.Fatalf("runonce %d: %v", i, err)
		}
	}
	failures, total, _ := svc.Batch.ListFailures(context.Background(), store.FailureFilter{Page: domain.Page{Size: 10}})
	if total != 1 {
		t.Fatalf("failures=%d want 1", total)
	}
	if failures[0].TaskType != "scheduler.collector" || failures[0].LastError != "boom" {
		t.Fatalf("failure=%+v", failures[0])
	}
	health := healthFor(t, sched, "collector")
	if health.Failures != 0 {
		t.Fatalf("failures not reset after dead letter: %d", health.Failures)
	}
	if health.DeadLettered != 1 {
		t.Fatalf("deadLettered=%d want 1", health.DeadLettered)
	}
}

// TestScheduler_RunOnce_RecordsSuccessCount confirms a successful tick updates
// the task's last run count.
func TestScheduler_RunOnce_RecordsSuccessCount(t *testing.T) {
	svc, clock, _ := newSchedulerSvc(t)
	sched := scheduler.New(svc, defaultCfg(t.TempDir()), clock, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var count int32
	sched.SetTick("evaluator", func(ctx context.Context) (int, error) {
		return int(atomic.AddInt32(&count, 1)), nil
	})
	if err := sched.RunOnce(context.Background(), "evaluator"); err != nil {
		t.Fatalf("runonce: %v", err)
	}
	h := healthFor(t, sched, "evaluator")
	if h.LastRunCount != 1 {
		t.Fatalf("lastRunCount=%d want 1", h.LastRunCount)
	}
}

// TestScheduler_Stop_GracefulShutdown starts and stops the scheduler cleanly.
func TestScheduler_Stop_GracefulShutdown(t *testing.T) {
	svc, clock, _ := newSchedulerSvc(t)
	sched := scheduler.New(svc, defaultCfg(t.TempDir()), clock, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !sched.Started() {
		t.Fatalf("not started")
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := sched.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func healthFor(t *testing.T, sched *scheduler.Scheduler, name string) scheduler.TaskHealth {
	t.Helper()
	for _, h := range sched.Health() {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("task %s not found", name)
	return scheduler.TaskHealth{}
}
