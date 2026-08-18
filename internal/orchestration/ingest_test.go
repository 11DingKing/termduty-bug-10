package orchestration_test

import (
	"context"
	"testing"
	"time"

	"termduty/internal/domain"
	"termduty/internal/orchestration"
	"termduty/internal/store"
)

// TestIngest_ProcessBatch_IdempotentNoDoubleProcess confirms processing a queue
// twice only persists each reading once.
func TestIngest_ProcessBatch_IdempotentNoDoubleProcess(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "P1")
	for i := 0; i < 2; i++ {
		sub := domain.ReadingSubmission{CollectorID: c.ID, Timestamp: clock.Now(), QueueCount: 3 + i}
		if _, err := svc.Ingest.Enqueue(context.Background(), sub, systemActor()); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	s1, err := svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10)
	if err != nil || s1.Processed != 2 {
		t.Fatalf("first process: %+v err=%v", s1, err)
	}
	s2, err := svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10)
	if err != nil || s2.Processed != 0 {
		t.Fatalf("second process should be idle: %+v err=%v", s2, err)
	}
	total, _ := st.Readings().Total(context.Background())
	if total != 2 {
		t.Fatalf("readings=%d want 2", total)
	}
}

// TestIngest_MalformedPayload_DeadLettered confirms an undecodable payload is
// recorded as a permanent failure for manual re-injection.
func TestIngest_MalformedPayload_DeadLettered(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "D1")
	item := domain.IngestItem{CollectorID: c.ID, Payload: []byte("not-json"), Status: domain.IngestPending, CreatedAt: clock.Now()}
	if err := st.Ingest().Enqueue(context.Background(), item); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	summary, err := svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if summary.DeadLettered != 1 || summary.Processed != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	failures, total, _ := svc.Batch.ListFailures(context.Background(), failureFilter())
	if total != 1 {
		t.Fatalf("failures=%d want 1", total)
	}
	if failures[0].TaskType != "ingest" {
		t.Fatalf("failure task type=%s", failures[0].TaskType)
	}
}

// TestIngest_ReinjectFailure_Requeues confirms a dead-lettered item is returned
// to pending for another attempt.
func TestIngest_ReinjectFailure_Requeues(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "Q1")
	item := domain.IngestItem{CollectorID: c.ID, Payload: []byte("not-json"), Status: domain.IngestPending, CreatedAt: clock.Now()}
	_ = st.Ingest().Enqueue(context.Background(), item)
	_, _ = svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10)
	failures, _, _ := svc.Batch.ListFailures(context.Background(), failureFilter())
	if len(failures) != 1 {
		t.Fatalf("failures=%d", len(failures))
	}
	_, err := svc.Batch.ReinjectFailure(context.Background(), failures[0].ID, opsActor())
	if err != nil {
		t.Fatalf("reinject: %v", err)
	}
	// The referenced ingest item should be pending again.
	got, _ := st.Ingest().Get(context.Background(), domain.IngestID(failures[0].TargetID))
	if got.Status != domain.IngestPending {
		t.Fatalf("ingest status=%s want pending", got.Status)
	}
}

// TestIngest_LeaseReclaim_RestartRecovers simulates a crash mid-lease and
// verifies the expired lease returns work to the pool.
func TestIngest_LeaseReclaim_RestartRecovers(t *testing.T) {
	svc, clock, st := newSvc(t)
	c := seedCollector(t, st, "L1")
	sub := domain.ReadingSubmission{CollectorID: c.ID, Timestamp: clock.Now(), QueueCount: 5}
	_, _ = svc.Ingest.Enqueue(context.Background(), sub, systemActor())
	leased, err := st.Ingest().LeaseBatch(context.Background(), clock.Now(), time.Minute, 10)
	if err != nil || len(leased) != 1 {
		t.Fatalf("lease: %v len=%d", err, len(leased))
	}
	// Crash: do not complete. Advance clock past TTL and reclaim.
	clock.Advance(2 * time.Minute)
	n, err := svc.Ingest.Reclaim(context.Background(), clock.Now())
	if err != nil || n != 1 {
		t.Fatalf("reclaim: %v n=%d", err, n)
	}
	summary, err := svc.Ingest.ProcessBatch(context.Background(), clock.Now(), time.Minute, 10)
	if err != nil || summary.Processed != 1 {
		t.Fatalf("process after reclaim: %+v err=%v", summary, err)
	}
	total, _ := st.Readings().Total(context.Background())
	if total != 1 {
		t.Fatalf("readings=%d want 1", total)
	}
}

func systemActor() orchestration.Actor {
	return orchestration.Actor{ID: "system", Role: domain.RoleSystem}
}

func opsActor() orchestration.Actor { return orchestration.Actor{ID: "ops", Role: domain.RoleOps} }

func failureFilter() store.FailureFilter { return store.FailureFilter{Page: domain.Page{Size: 50}} }
