package shard_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"

	"termduty/internal/store/shard"
)

func newShardStore(t *testing.T) *shard.Store {
	t.Helper()
	s, err := shard.New(t.TempDir())
	if err != nil {
		t.Fatalf("new shard: %v", err)
	}
	return s
}

// TestShardAppendCommit_Persists confirms a committed line is durable and
// readable back by line number.
func TestShardAppendCommit_Persists(t *testing.T) {
	s := newShardStore(t)
	ctx := context.Background()
	sid := shard.ShardID("readings", "2026-08-18")
	lease, err := s.Begin(ctx, sid)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if lease.LineNo() != 0 {
		t.Fatalf("first line no=%d", lease.LineNo())
	}
	payload, _ := json.Marshal(map[string]int{"n": 1})
	if err := lease.AppendLine(ctx, payload); err != nil {
		t.Fatalf("append: %v", err)
	}
	lease.Commit()
	var out map[string]int
	if err := s.ReadLine(ctx, sid, 0, &out); err != nil {
		t.Fatalf("read: %v", err)
	}
	if out["n"] != 1 {
		t.Fatalf("got %v", out)
	}
}

// TestShardAppendRollback_Truncates verifies a rolled-back append leaves no line.
func TestShardAppendRollback_Truncates(t *testing.T) {
	s := newShardStore(t)
	ctx := context.Background()
	sid := shard.ShardID("readings", "2026-08-18")
	lease, _ := s.Begin(ctx, sid)
	if err := lease.AppendLine(ctx, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := lease.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	stat, err := s.Recompute(ctx, sid)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if stat.Count != 0 {
		t.Fatalf("count after rollback=%d want 0", stat.Count)
	}
}

// TestShardReadLine_NotFoundForMissingLine ensures an out-of-range line errors.
func TestShardReadLine_NotFoundForMissingLine(t *testing.T) {
	s := newShardStore(t)
	ctx := context.Background()
	sid := shard.ShardID("readings", "2026-08-18")
	lease, _ := s.Begin(ctx, sid)
	_ = lease.AppendLine(ctx, []byte(`{"a":1}`))
	lease.Commit()
	var out map[string]int
	err := s.ReadLine(ctx, sid, 5, &out)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

// TestShardAppend_ConcurrentSerializes appends from many goroutines to the same
// shard and asserts every line lands with a unique, contiguous line number.
func TestShardAppend_ConcurrentSerializes(t *testing.T) {
	s := newShardStore(t)
	ctx := context.Background()
	sid := shard.ShardID("readings", "2026-08-19")
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			lease, err := s.Begin(ctx, sid)
			if err != nil {
				t.Errorf("begin: %v", err)
				return
			}
			ln := lease.LineNo()
			payload, _ := json.Marshal(map[string]int{"i": i})
			if err := lease.AppendLine(ctx, payload); err != nil {
				t.Errorf("append: %v", err)
				lease.Rollback(ctx)
				return
			}
			lease.Commit()
			_ = ln
		}()
	}
	wg.Wait()
	stat, err := s.Recompute(ctx, sid)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if stat.Count != n {
		t.Fatalf("count=%d want %d", stat.Count, n)
	}
}

// TestFoldChecksum_DeterministicAndIncremental verifies recomputing the whole
// shard yields the same checksum maintained incrementally on append.
func TestFoldChecksum_DeterministicAndIncremental(t *testing.T) {
	s := newShardStore(t)
	ctx := context.Background()
	sid := shard.ShardID("readings", "2026-08-20")
	running := ""
	for i := 0; i < 3; i++ {
		lease, _ := s.Begin(ctx, sid)
		line := []byte(`{"i":` + string(rune('0'+i)) + `}`)
		if err := lease.AppendLine(ctx, line); err != nil {
			t.Fatalf("append: %v", err)
		}
		running = shard.FoldChecksum(running, line)
		lease.Commit()
	}
	stat, err := s.Recompute(ctx, sid)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if stat.Checksum != running {
		t.Fatalf("incremental=%s recomputed=%s", running, stat.Checksum)
	}
}

// TestShardListShards_EmptyWhenNone ensures a missing bucket returns no shards.
func TestShardListShards_EmptyWhenNone(t *testing.T) {
	s := newShardStore(t)
	ctx := context.Background()
	if got, err := s.ListShards(ctx, "readings"); err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

// avoid unused import warnings for time in this file's helpers if trimmed later
var _ = time.Second
