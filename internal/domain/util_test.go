package domain_test

import (
	"testing"
	"time"
)

func parseFake(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// TestParseFake_NoOp keeps the helper exercised.
func TestParseFake_NoOp(t *testing.T) {
	if parseFake("").IsZero() == false {
		t.Fatalf("expected zero")
	}
}
