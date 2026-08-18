package orchestration

import (
	"context"
	"log/slog"

	"termduty/internal/domain"
	"termduty/internal/store"
)

// AdminService handles single-object create/update/delete for collectors and
// rules. These are center-operations actions and are audited.
type AdminService struct {
	store *store.Store
	clock domain.Clock
	audit *AuditRecorder
	log   *slog.Logger
}

// CreateCollector registers a new collection point.
func (s *AdminService) CreateCollector(ctx context.Context, c domain.Collector, actor Actor) (domain.Collector, error) {
	if err := validateCollector(c); err != nil {
		return domain.Collector{}, err
	}
	if c.ID == "" {
		c.ID = domain.CollectorID(newID())
	}
	if c.Status == "" {
		c.Status = domain.CollectorStatusActive
	}
	c.Version = 1
	now := s.clock.Now()
	c.CreatedAt = now
	c.UpdatedAt = now
	if err := s.store.Collectors().Create(ctx, c); err != nil {
		return domain.Collector{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "collector.create", "collector", string(c.ID), map[string]any{"code": c.Code, "kind": string(c.Kind)})
	return c, nil
}

// UpdateCollector applies a full update with optimistic concurrency.
func (s *AdminService) UpdateCollector(ctx context.Context, c domain.Collector, actor Actor) (domain.Collector, error) {
	if err := validateCollector(c); err != nil {
		return domain.Collector{}, err
	}
	if err := s.store.Collectors().Update(ctx, &c); err != nil {
		return domain.Collector{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "collector.update", "collector", string(c.ID), map[string]any{"code": c.Code, "version": c.Version})
	return c, nil
}

func validateCollector(c domain.Collector) error {
	if c.Code == "" || c.Name == "" {
		return domain.ErrValidation
	}
	if !c.Kind.Valid() {
		return domain.ErrValidation
	}
	if c.Status != "" && !c.Status.Valid() {
		return domain.ErrValidation
	}
	return nil
}

// CreateRule registers a new agreed-range rule.
func (s *AdminService) CreateRule(ctx context.Context, r domain.Rule, actor Actor) (domain.Rule, error) {
	if err := validateRule(r); err != nil {
		return domain.Rule{}, err
	}
	if r.ID == "" {
		r.ID = newID()
	}
	r.Version = 1
	now := s.clock.Now()
	r.CreatedAt = now
	r.UpdatedAt = now
	if err := s.store.Rules().Create(ctx, r); err != nil {
		return domain.Rule{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "rule.create", "rule", r.ID, map[string]any{"metric": string(r.Metric), "severity": string(r.Severity)})
	return r, nil
}

// UpdateRule applies a full rule update with optimistic concurrency.
func (s *AdminService) UpdateRule(ctx context.Context, r domain.Rule, actor Actor) (domain.Rule, error) {
	if err := validateRule(r); err != nil {
		return domain.Rule{}, err
	}
	if err := s.store.Rules().Update(ctx, &r); err != nil {
		return domain.Rule{}, err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "rule.update", "rule", r.ID, map[string]any{"version": r.Version})
	return r, nil
}

// DeleteRule removes a rule if the expected version matches.
func (s *AdminService) DeleteRule(ctx context.Context, id string, version int64, actor Actor) error {
	if err := s.store.Rules().Delete(ctx, id, version); err != nil {
		return err
	}
	s.audit.bestEffort(ctx, actor.ID, actor.Role, "rule.delete", "rule", id, map[string]any{"version": version})
	return nil
}

func validateRule(r domain.Rule) error {
	if !r.Metric.Valid() {
		return domain.ErrValidation
	}
	if !r.Severity.Valid() {
		return domain.ErrValidation
	}
	if r.WindowSeconds < 0 {
		return domain.ErrValidation
	}
	if r.MinValue != nil && r.MaxValue != nil && *r.MinValue > *r.MaxValue {
		return domain.ErrValidation
	}
	if r.Metric == domain.MetricFaultCode && r.FaultTrigger == "" {
		return domain.ErrValidation
	}
	return nil
}
