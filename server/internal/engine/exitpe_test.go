package engine

import "testing"

// withCloses sets the quarter-end closes of q, oldest first; 0 means none.
func withCloses(q []Quarter, closes ...float64) []Quarter {
	for i, c := range closes {
		if c != 0 {
			q[i].Close = ptr(c)
		}
	}
	return q
}

func TestTypicalPE(t *testing.T) {
	// TTM EPS is 4 from the fourth quarter on, so the P/Es are close/4.
	q := withCloses(quarters(1, 1, 1, 1, 1, 1, 1, 1), 0, 0, 0, 40, 60, 80, 100, 0)
	odd := TypicalPE(q, 120, 4, 12) // quarters 3..7: P/Es 10, 15, 20, 25 and today's 30
	near(t, "median of an odd count", wantSome(t, "typical", odd.PE), 20, 1e-12)
	if odd.Months != 12 || odd.Points != 5 {
		t.Errorf("months, points = %d, %d, want 12, 5", odd.Months, odd.Points)
	}
	even := TypicalPE(q, 120, 4, 9) // quarters 4..7: P/Es 15, 20, 25 and today's 30
	near(t, "the conventional median of an even count", wantSome(t, "typical", even.PE), 22.5, 1e-12)

	if got := TypicalPE(q, 120, 4, 0).Months; got != 36 {
		t.Errorf("default months = %d, want 36", got)
	}
	if got := TypicalPE(q, 120, 4, 100).Months; got != 36 {
		t.Errorf("months are capped at 36, got %d", got)
	}
	short := TypicalPE(q, 120, 4, 36) // starts at the first quarter
	if short.Points != 5 {
		t.Errorf("short history uses %d points, want 5", short.Points)
	}
	none := TypicalPE(quarters(1, 1), 120, -1, 12)
	wantNone(t, "typical P/E without any P/E", none.PE, ReasonInsufficientHistory)
}

func TestEstimatedExitPE(t *testing.T) {
	tests := []struct {
		name            string
		price, eps      float64
		weighted, typic *float64
		want            float64
		rule            ExitPERule
	}{
		// g0 = 18%: gExit = 6% + 12% × 0.5 = 12%; 15 + 15 × 6/12 = 22.5.
		{"fade halves the premium over one half-life", 300, 10, ptr(0.18), ptr(25.0), 22.5, RuleFade},
		{"a slow grower above its typical P/E", 70, 2.8, ptr(0.05), ptr(21.0), 21, RuleSlowGrowerTypical},
		{"a slow grower near its typical P/E", 70, 2.8, ptr(0.05), ptr(24.5), 25, RuleSlowGrowerKeep},
		{"a shrinking company", 70, 2.8, ptr(-0.2), nil, 25, RuleSlowGrowerKeep},
		{"a low P/E is kept", 120, 10, ptr(0.3), ptr(10.0), 12, RuleLowPEKeep},
		{"no weighted trend counts as slow growth", 70, 2.8, nil, ptr(20.0), 20, RuleSlowGrowerTypical},
		{"never below 6", 30, 10, ptr(0.02), ptr(2.0), 6, RuleSlowGrowerTypical},
		{"rounded to 0.5", 100, 4.1, ptr(0.06), nil, 24.5, RuleSlowGrowerKeep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimatedExitPE(tt.price, tt.eps, 3, tt.weighted, tt.typic)
			near(t, "exit P/E", wantSome(t, "exit P/E", got.PE), tt.want, 1e-12)
			if got.Rule == nil || *got.Rule != tt.rule {
				t.Errorf("rule = %v, want %q", got.Rule, tt.rule)
			}
		})
	}

	bad := EstimatedExitPE(100, 0, 3, ptr(0.1), nil)
	wantNone(t, "zero EPS", bad.PE, ReasonNeedsPositiveEPS)
	if bad.Rule != nil {
		t.Errorf("rule = %q, want none without a value", *bad.Rule)
	}
	wantNone(t, "g0 without a trend", EstimatedExitPE(100, 4, 3, nil, nil).G0, ReasonInsufficientHistory)
}

// Just above the 6% floor, the premium still fades by the half-life, not
// to nothing by cancellation.
func TestEstimatedExitPEJustAboveTheFloor(t *testing.T) {
	got := EstimatedExitPE(300, 10, 3, ptr(0.060000000000000005), ptr(25.0))
	near(t, "exit P/E", wantSome(t, "exit P/E", got.PE), 22.5, 0)
}
