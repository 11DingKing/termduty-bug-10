package orchestration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// QueryService holds the read paths that compose more than one store call
// (statistics and streaming exports). Plain listings go straight to the store
// repositories from the HTTP layer, so this type carries no thin pass-through
// wrappers.
type QueryService struct {
	store *store.Store
	clock domain.Clock
	log   *slog.Logger
}

// ExportReadings streams matching readings as JSONL into w and returns the count.
func (s *QueryService) ExportReadings(ctx context.Context, f store.ReadingFilter, w io.Writer) (int, error) {
	count := 0
	enc := json.NewEncoder(w)
	err := s.store.Readings().Export(ctx, f, func(rd domain.Reading) error {
		if err := enc.Encode(rd); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

// Stats aggregates headline counters for the overview.
func (s *QueryService) Stats(ctx context.Context) (StatsSummary, error) {
	out := StatsSummary{AlertsByState: map[domain.AlertState]int64{}}
	if _, total, err := s.store.Collectors().List(ctx, store.CollectorFilter{Page: domain.Page{Size: 1}}); err == nil {
		out.Collectors = total
	}
	counts, err := s.store.Alerts().CountByState(ctx)
	if err != nil {
		return out, err
	}
	out.AlertsByState = counts
	out.ActiveAlerts = counts[domain.AlertStateOpen] + counts[domain.AlertStateAssigned] + counts[domain.AlertStateHandling]
	if total, err := s.store.Readings().Total(ctx); err == nil {
		out.Readings = total
	}
	if n, err := s.store.Ingest().PendingCount(ctx); err == nil {
		out.PendingIngest = n
	}
	if n, err := s.store.Ingest().LeasedCount(ctx); err == nil {
		out.LeasedIngest = n
	}
	if failures, _, err := s.store.Failures().List(ctx, store.FailureFilter{Resolved: boolPtr(false), Page: domain.Page{Size: 1}}); err == nil {
		out.DeadLettered = int64(len(failures))
	}
	if manifest, err := s.store.Readings().Manifest(ctx); err == nil {
		out.Shards = len(manifest)
	}
	return out, nil
}
