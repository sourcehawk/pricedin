package engine

import (
	"slices"
	"testing"
)

func gridAt(mode BaselineMode, g Number) GridInput {
	return GridInput{
		Investment: grmn,
		ExitPE:     20,
		Required:   Some(0.2388),
		Baseline:   ActiveBaseline{Mode: mode, Growth: g},
		Weighted:   Some(0.1595),
		Today:      Some(0.1224),
		Trailing:   Some(0.1676),
	}
}

func TestReturnGridYouCellMatchesTheBaseline(t *testing.T) {
	grid := wantSome(t, "grid", ReturnGrid(gridAt(BaselineWeighted, Some(0.1595))))
	you := wantSome(t, "you", grid.You)
	row := grid.Rows[you.Row]
	if !row.Exact || row.Growth != 0.1595 {
		t.Fatalf("YOU row = %+v, want the exact baseline row at 0.1595", row)
	}
	if grid.Columns[you.Column].ExitPE != 20 {
		t.Errorf("YOU column = %v, want the exit P/E 20", grid.Columns[you.Column].ExitPE)
	}
	want := Project(grmn, 0.1595, 20)
	cell := row.Cells[you.Column]
	near(t, "YOU yearly return", cell.YearlyReturn, want.YearlyReturn, 0)
	near(t, "YOU exit price", cell.ExitPrice, want.ExitPrice, 0)
	if !slices.Contains(row.Tags, TagWeighted) {
		t.Errorf("the exact row tags = %v, want the weighted tag", row.Tags)
	}
	if !slices.IsSortedFunc(grid.Rows, func(a, b GridRow) int { return compareFloat(a.Growth, b.Growth) }) {
		t.Error("rows are not sorted by growth")
	}
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func TestReturnGridMarksAnExistingRowExact(t *testing.T) {
	grid := wantSome(t, "grid", ReturnGrid(gridAt(BaselineCustom, Some(0.1))))
	exact := 0
	for _, r := range grid.Rows {
		if r.Exact {
			exact++
			if r.Growth != 0.1 || !slices.Contains(r.Tags, TagCustom) {
				t.Errorf("exact row = %+v, want 10%% with the custom tag", r)
			}
		}
	}
	if exact != 1 {
		t.Errorf("%d exact rows, want 1", exact)
	}
	if grid.Step != 0.025 {
		t.Errorf("step = %v, want 0.025", grid.Step)
	}
}

func TestReturnGridTagsTheOtherBaselinesOnTheirNearestRow(t *testing.T) {
	grid := wantSome(t, "grid", ReturnGrid(gridAt(BaselineToday, Some(0.1224))))
	tagged := map[GridTag]float64{}
	for _, r := range grid.Rows {
		for _, tag := range r.Tags {
			if _, dup := tagged[tag]; dup {
				t.Errorf("tag %q appears twice", tag)
			}
			tagged[tag] = r.Growth
		}
	}
	want := map[GridTag]float64{TagRequired: 0.25, TagWeighted: 0.15, TagToday: 0.1224, TagTrailing: 0.175}
	for tag, g := range want {
		if got, ok := tagged[tag]; !ok || got != g {
			t.Errorf("tag %q on row %v, want %v", tag, got, g)
		}
	}
	if _, ok := tagged[TagCustom]; ok {
		t.Error("the custom tag appears without the custom baseline")
	}
}

func TestReturnGridCellsCompareWithTheTarget(t *testing.T) {
	grid := wantSome(t, "grid", ReturnGrid(gridAt(BaselineWeighted, Some(0.1595))))
	for _, r := range grid.Rows {
		for j, c := range r.Cells {
			if c.VsTarget != c.YearlyReturn-grmn.TargetReturn || c.Tone != ToneOf(c.VsTarget) {
				t.Errorf("row %v column %v: %+v does not compare with the target", r.Growth, grid.Columns[j].ExitPE, c)
			}
		}
	}
	pes := make([]float64, 0, len(grid.Columns))
	for _, c := range grid.Columns {
		pes = append(pes, c.ExitPE)
	}
	if !slices.IsSorted(pes) || len(slices.Compact(slices.Clone(pes))) != len(pes) || !slices.Contains(pes, 20) {
		t.Errorf("columns %v must be sorted, unique and contain the exit P/E", pes)
	}
}

func TestReturnGridWithoutValues(t *testing.T) {
	in := gridAt(BaselineTrailing, None[float64](ReasonInsufficientHistory))
	grid := wantSome(t, "grid", ReturnGrid(in))
	wantNone(t, "you", grid.You, ReasonInsufficientHistory)
	wantNone(t, "column sensitivity", grid.ColumnSensitivity, ReasonInsufficientHistory)
	for _, r := range grid.Rows {
		if r.Exact {
			t.Errorf("row %v is exact without a baseline", r.Growth)
		}
	}

	in = gridAt(BaselineWeighted, Some(0.1595))
	in.EPS = 0
	wantNone(t, "grid with zero EPS", ReturnGrid(in), ReasonNeedsPositiveEPS)
	in = gridAt(BaselineWeighted, Some(0.1595))
	in.ExitPE = 0
	wantNone(t, "grid with a zero exit P/E", ReturnGrid(in), ReasonNeedsPositiveEPS)
}
