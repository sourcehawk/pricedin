package engine

import (
	"math"
	"slices"
)

// GridInput is what the return grid needs.
type GridInput struct {
	Investment
	ExitPE   float64        `json:"exitPe"` // the exit P/E in use
	Required Number         `json:"required"`
	Baseline ActiveBaseline `json:"baseline"`
	Weighted Number         `json:"weighted"`
	Today    Number         `json:"today"`
	Trailing Number         `json:"trailing"`
}

// ActiveBaseline is the baseline in use and its growth rate.
type ActiveBaseline struct {
	Mode   BaselineMode `json:"mode"`
	Growth Number       `json:"growth"`
}

// GridTag marks the row of a growth rate.
type GridTag string

// The row tags. The custom tag appears only when the custom baseline is
// active.
const (
	TagRequired GridTag = "required"
	TagWeighted GridTag = "weighted"
	TagToday    GridTag = "today"
	TagTrailing GridTag = "trailing"
	TagCustom   GridTag = "custom"
)

// Grid is the return grid: rows are EPS growth rates, columns are exit
// P/Es, and each cell is the outcome of that pair.
type Grid struct {
	Step    float64      `json:"step"` // between the regular rows
	Columns []GridColumn `json:"columns"`
	Rows    []GridRow    `json:"rows"`
	// You is the user's own cell: the exact baseline row at the exit P/E in
	// use. It is missing without a baseline.
	You Maybe[GridCell] `json:"you"`
	// ColumnSensitivity is how much yearly return one row step adds in the
	// exit P/E column in use, near the baseline.
	ColumnSensitivity Maybe[Sensitivity] `json:"columnSensitivity"`
}

// GridColumn is one exit P/E.
type GridColumn struct {
	ExitPE float64 `json:"exitPe"`
	Today  bool    `json:"today"` // within 0.5 of today's P/E
}

// GridRow is one growth rate with its outcomes, one per column.
type GridRow struct {
	Growth    float64       `json:"growth"`
	EPSAtExit float64       `json:"epsAtExit"`
	Exact     bool          `json:"exact"` // the row of the exact baseline growth
	Tags      []GridTag     `json:"tags"`  // the baselines whose value is nearest to this row
	Cells     []GridOutcome `json:"cells"`
}

// GridOutcome is one cell of the grid.
type GridOutcome struct {
	ExitPrice    float64 `json:"exitPrice"`
	TotalReturn  float64 `json:"totalReturn"`
	YearlyReturn float64 `json:"yearlyReturn"`
	VsTarget     float64 `json:"vsTarget"` // yearly return minus the target return
	Tone         Tone    `json:"tone"`
}

// GridCell is a position in the grid.
type GridCell struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

// Sensitivity is the change in yearly return for one step of growth.
type Sensitivity struct {
	GrowthStep   float64 `json:"growthStep"`
	ReturnChange float64 `json:"returnChange"`
}

// ReturnGrid returns the return grid. The rows adapt to the growth rates of
// the baselines and the requirement; the columns spread around the exit
// P/E in use. A row at the exact baseline growth carries the baseline's tag
// and the YOU cell; the other tags sit on their nearest row. The grid is
// missing when EPS or the exit P/E is not positive.
func ReturnGrid(in GridInput) Maybe[Grid] {
	if !(in.EPS > 0) || !(in.ExitPE > 0) {
		return None[Grid](ReasonNeedsPositiveEPS)
	}
	pe := in.ExitPE
	g := in.Baseline.Growth
	lo, hi := 0.0, 0.3
	for _, m := range []Number{in.Required, in.Today, in.Weighted, in.Trailing, g} {
		if m.Ok() {
			lo, hi = math.Min(lo, m.Get()), math.Max(hi, m.Get())
		}
	}
	lo, hi = math.Max(-0.4, lo), math.Min(1.5, hi)
	step := 0.2
	for _, st := range []float64{0.025, 0.05, 0.1, 0.2} {
		if math.Ceil(hi/st)-math.Floor(lo/st) <= 13 {
			step = st
			break
		}
	}
	var regular []float64
	for i := math.Floor(lo / step); i <= math.Ceil(hi/step); i++ {
		regular = append(regular, toFixed(i*step, 4))
	}

	cols := gridColumns(pe)
	youCol := slices.Index(cols, pe)

	gOk := g.Ok() && growthInRange(g.Get())
	rows := make([]GridRow, 0, len(regular)+1)
	for _, v := range regular {
		rows = append(rows, GridRow{Growth: v})
	}
	if gOk {
		at := slices.IndexFunc(rows, func(r GridRow) bool { return math.Abs(r.Growth-g.Get()) < 1e-9 })
		if at >= 0 {
			rows[at].Exact = true
		} else {
			at = insertIndex(rows, g.Get())
			rows = slices.Insert(rows, at, GridRow{Growth: g.Get(), Exact: true})
		}
	}
	youRow := slices.IndexFunc(rows, func(r GridRow) bool { return r.Exact })
	growths := make([]float64, len(rows))
	for i := range rows {
		growths[i] = rows[i].Growth
		rows[i].Tags = []GridTag{}
	}
	place := func(tag GridTag, active bool, v Number) {
		if active {
			if youRow >= 0 {
				rows[youRow].Tags = append(rows[youRow].Tags, tag)
			}
			return
		}
		if v.Ok() {
			i := nearest(growths, v.Get())
			rows[i].Tags = append(rows[i].Tags, tag)
		}
	}
	mode := in.Baseline.Mode
	place(TagRequired, false, in.Required)
	place(TagWeighted, mode == BaselineWeighted, in.Weighted)
	place(TagToday, mode == BaselineToday, in.Today)
	place(TagTrailing, mode == BaselineTrailing, in.Trailing)
	if mode == BaselineCustom {
		place(TagCustom, true, g)
	}

	n := float64(in.Years)
	for i := range rows {
		rows[i].EPSAtExit = in.EPS * math.Pow(1+rows[i].Growth, n)
		rows[i].Cells = make([]GridOutcome, len(cols))
		for j, c := range cols {
			p := Project(in.Investment, rows[i].Growth, c)
			diff := p.YearlyReturn - in.TargetReturn
			rows[i].Cells[j] = GridOutcome{ExitPrice: p.ExitPrice, TotalReturn: p.TotalReturn, YearlyReturn: p.YearlyReturn, VsTarget: diff, Tone: ToneOf(diff)}
		}
	}

	out := Grid{Step: step, Rows: rows, Columns: make([]GridColumn, len(cols))}
	nowPE := in.Price / in.EPS
	for j, c := range cols {
		out.Columns[j] = GridColumn{ExitPE: c, Today: math.Abs(c-nowPE) < 0.5}
	}
	reason := ReasonGrowthOutOfRange
	if !g.Ok() {
		reason = g.Reason
	}
	out.You = None[GridCell](reason)
	out.ColumnSensitivity = None[Sensitivity](reason)
	if gOk {
		out.You = Some(GridCell{Row: youRow, Column: youCol})
		at := func(ri int) float64 { return Project(in.Investment, regular[ri], cols[youCol]).YearlyReturn }
		i := min(max(nearest(regular, g.Get()), 1), len(regular)-1)
		out.ColumnSensitivity = Some(Sensitivity{GrowthStep: regular[i] - regular[i-1], ReturnChange: at(i) - at(i-1)})
	}
	return Some(out)
}

// gridColumns spreads exit P/Es around pe: whole numbers below 40, steps
// of 5 above, and pe itself.
func gridColumns(pe float64) []float64 {
	factors := []float64{0.55, 0.7, 0.8, 0.9, 1, 1.12, 1.25, 1.4, 1.6}
	cols := make([]float64, 0, len(factors))
	for _, f := range factors {
		v := pe
		if f != 1 {
			v = pe * f
			if v < 40 {
				v = jsRound(v)
			} else {
				v = jsRound(v/5) * 5
			}
		}
		if !slices.Contains(cols, v) {
			cols = append(cols, v)
		}
	}
	slices.Sort(cols)
	return cols
}

// nearest returns the index of the value in vs nearest to v; the first one
// wins a tie.
func nearest(vs []float64, v float64) int {
	b := 0
	for i, x := range vs {
		if math.Abs(x-v) < math.Abs(vs[b]-v) {
			b = i
		}
	}
	return b
}

// insertIndex returns the index at which g keeps rows sorted by growth.
func insertIndex(rows []GridRow, g float64) int {
	i, _ := slices.BinarySearchFunc(rows, g, func(r GridRow, v float64) int {
		switch {
		case r.Growth < v:
			return -1
		case r.Growth > v:
			return 1
		default:
			return 0
		}
	})
	return i
}
