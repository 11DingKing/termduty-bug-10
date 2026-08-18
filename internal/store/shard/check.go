package shard

import (
	"context"
	"errors"
	"io/fs"
	"os"
)

// CheckRoot verifies the shard root directory is present and writable.
func (s *Store) CheckRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(s.root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return os.MkdirAll(s.root, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return errors.New("shard root is not a directory")
	}
	probe := s.root + "/.healthprobe"
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	os.Remove(probe)
	return nil
}
