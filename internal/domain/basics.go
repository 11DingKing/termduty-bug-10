package domain

import (
	"errors"
	"time"
)

// Sentinel domain errors. They are wrapped with %w along the call chain and
// matched at the HTTP boundary with errors.Is, so the status code reflects the
// real reason instead of a generic 500.
var (
	ErrNotFound           = errors.New("entity not found")
	ErrAlreadyExists      = errors.New("entity already exists")
	ErrIllegalTransition  = errors.New("illegal state transition")
	ErrAlreadyAssigned    = errors.New("alert already accepted by another handler")
	ErrConflict           = errors.New("optimistic write conflict")
	ErrSuppressed         = errors.New("alert suppressed for the same object and problem")
	ErrValidation         = errors.New("validation error")
	ErrCollectorDisabled  = errors.New("collector is disabled")
	ErrPermanentFailure   = errors.New("permanent failure")
	ErrOutOfRange         = errors.New("page or parameter out of range")
	ErrSchemaIncompatible = errors.New("schema version incompatible")
)

// Clock abstracts the wall clock so background tasks and tests can advance time
// deterministically without real sleeps or network waits.
type Clock interface{ Now() time.Time }

// RealClock returns the system wall clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// FakeClock is a controllable clock intended for tests.
type FakeClock struct{ t time.Time }

func NewFakeClock(t time.Time) *FakeClock    { return &FakeClock{t: t} }
func (f *FakeClock) Now() time.Time          { return f.t }
func (f *FakeClock) Advance(d time.Duration) { f.t = f.t.Add(d) }
func (f *FakeClock) Set(t time.Time)         { f.t = t }

// Page is an offset-based pagination request, normalised and validated by the
// store layer so callers cannot read past the agreed range.
type Page struct {
	Offset int `json:"offset"`
	Size   int `json:"size"`
}

// Normalize clamps size into the [1, max] window and zeroes negative offsets.
func (p Page) Normalize(max int) Page {
	size := p.Size
	if size <= 0 {
		size = max
	}
	if size > max {
		size = max
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	return Page{Offset: offset, Size: size}
}
