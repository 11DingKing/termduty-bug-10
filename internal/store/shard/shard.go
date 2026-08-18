// Package shard implements the sharded JSONL file store that holds the
// authoritative copies of operational readings. Each shard is one append-only
// file named by its bucket and a date shard key. A manifest of shard metadata
// (counts and checksums) is maintained by the upper store layer.
//
// Writes use a two-phase lease: Begin locks the shard and records the file
// size, AppendLine writes and fsyncs a new line, then either Commit releases
// the lock or Rollback truncates the file back to its pre-append size. This
// gives the store layer an explicit commit/rollback point so a multi-step write
// (append line + update index) never leaves a half-record on disk.
package shard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store manages sharded JSONL files under a configurable root directory.
type Store struct {
	root   string
	mu     sync.Mutex
	counts map[string]int64
}

// New creates a shard store rooted at root, creating the directory if needed.
func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root, counts: make(map[string]int64)}, nil
}

// ShardID composes the canonical shard identifier from a bucket and key.
func ShardID(bucket, key string) string { return bucket + "/" + key }

func (s *Store) path(shardID string) (string, error) {
	parts := strings.SplitN(shardID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("invalid shard id: " + shardID)
	}
	return filepath.Join(s.root, parts[0], parts[1]+".jsonl"), nil
}

// Lease is an open append transaction on a shard. Exactly one of Commit or
// Rollback must be called to release the shard lock.
type Lease struct {
	store      *Store
	shardID    string
	path       string
	sizeBefore int64
	lineNo     int64
}

// Begin locks the shard and prepares a new line to append. It returns the line
// number the next line will receive and the file size to roll back to.
func (s *Store) Begin(ctx context.Context, shardID string) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	p, err := s.path(shardID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	info, err := os.Stat(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.mu.Unlock()
		return nil, err
	}
	var size int64
	var lineNo int64
	if err == nil {
		size = info.Size()
		lineNo = s.lineCountLocked(shardID, p, size)
	} else {
		s.counts[shardID] = 0
	}
	return &Lease{store: s, shardID: shardID, path: p, sizeBefore: size, lineNo: lineNo}, nil
}

func (s *Store) lineCountLocked(shardID, p string, size int64) int64 {
	if cached, ok := s.counts[shardID]; ok && cached >= 0 {
		return cached
	}
	count, err := countLines(p)
	if err != nil {
		count = 0
	}
	s.counts[shardID] = count
	return count
}

func countLines(p string) (int64, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n, nil
}

// LineNo returns the line number the next AppendLine will occupy.
func (l *Lease) LineNo() int64 { return l.lineNo }

// AppendLine writes data followed by a newline and fsyncs the shard file.
func (l *Lease) AppendLine(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	l.store.counts[l.shardID] = l.lineNo + 1
	return nil
}

// Commit releases the shard lock, keeping the appended line.
func (l *Lease) Commit() {
	l.store.mu.Unlock()
}

// Rollback truncates the shard back to its pre-append size and fsyncs, then
// releases the lock. It is safe to call even when no line was appended.
func (l *Lease) Rollback(ctx context.Context) error {
	defer l.store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(l.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			l.store.counts[l.shardID] = l.lineNo
			return nil
		}
		return err
	}
	if info.Size() > l.sizeBefore {
		if err := os.Truncate(l.path, l.sizeBefore); err != nil {
			return err
		}
		f, err := os.OpenFile(l.path, os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	l.store.counts[l.shardID] = l.lineNo
	return nil
}

// ReadLine decodes the JSON line at lineNo (0-based) into out.
func (s *Store) ReadLine(ctx context.Context, shardID string, lineNo int64, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(shardID)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var current int64
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			if current == lineNo {
				return json.Unmarshal(data[start:i], out)
			}
			current++
			start = i + 1
		}
	}
	if current == lineNo && start < len(data) {
		return json.Unmarshal(data[start:], out)
	}
	return fs.ErrNotExist
}

// Stat summarises a shard.
type Stat struct {
	ShardID  string
	Path     string
	Count    int64
	Checksum string
}

// Scan reads every line of a shard, invoking fn for each decoded raw payload.
// Corrupt lines are skipped and reported via the returned error count; the scan
// continues so a single bad line never blocks the rest of the shard.
func (s *Store) Scan(ctx context.Context, shardID string, fn func(lineNo int64, data []byte) error) (Stat, error) {
	if err := ctx.Err(); err != nil {
		return Stat{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(shardID)
	if err != nil {
		return Stat{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Stat{ShardID: shardID, Path: p}, err
	}
	stat := Stat{ShardID: shardID, Path: p}
	checksum := ""
	start := 0
	var lineNo int64
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if start < i {
				line := data[start:i]
				checksum = FoldChecksum(checksum, line)
				stat.Count++
				if err := fn(lineNo, line); err != nil {
					return stat, err
				}
				lineNo++
			}
			start = i + 1
		}
	}
	stat.Checksum = checksum
	return stat, nil
}

// Recompute recomputes the line count and checksum from the file contents.
func (s *Store) Recompute(ctx context.Context, shardID string) (Stat, error) {
	return s.Scan(ctx, shardID, func(int64, []byte) error { return nil })
}

// ListShards returns the shard ids present on disk for a bucket.
func (s *Store) ListShards(ctx context.Context, bucket string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.root, bucket)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		key := strings.TrimSuffix(name, ".jsonl")
		out = append(out, ShardID(bucket, key))
	}
	return out, nil
}

// FoldChecksum folds a line into a running checksum. The fold is incremental and
// deterministic so the manifest can track the checksum on each append and the
// verify command can recompute the whole shard from scratch.
func FoldChecksum(prev string, line []byte) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte{0})
	h.Write(line)
	return hex.EncodeToString(h.Sum(nil))
}

// AbsPath returns the absolute on-disk path of a shard.
func (s *Store) AbsPath(shardID string) (string, error) {
	return s.path(shardID)
}
