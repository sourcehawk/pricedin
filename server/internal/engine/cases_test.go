package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

// casesPath is the shared case file that the TypeScript engine runs too.
var casesPath = filepath.Join("..", "..", "..", "testdata", "engine", "cases.json")

type sharedCase struct {
	Name      string          `json:"name"`
	Fn        string          `json:"fn"`
	Input     json.RawMessage `json:"input"`
	Expected  json.RawMessage `json:"expected"`
	Tolerance float64         `json:"tolerance"`
	Note      string          `json:"note"`
	Select    string          `json:"select"`
}

type caseFile struct {
	Version int          `json:"version"`
	Cases   []sharedCase `json:"cases"`
}

// runner decodes a case input into In and calls f.
func runner[In any](f func(In) (any, error)) func(json.RawMessage) (any, error) {
	return func(raw json.RawMessage) (any, error) {
		var in In
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		return f(in)
	}
}

type investmentAt struct {
	Investment
	ExitPE float64 `json:"exitPe"`
}

type investmentGrowing struct {
	Investment
	Growth float64 `json:"growth"`
	ExitPE float64 `json:"exitPe"`
}

// runners maps each fn name of the case file to the Go function.
var runners = map[string]func(json.RawMessage) (any, error){
	"requiredGrowth": runner(func(in investmentAt) (any, error) { return RequiredGrowth(in.Investment, in.ExitPE), nil }),
	"requirement":    runner(func(in investmentAt) (any, error) { return Requirement(in.Investment, in.ExitPE), nil }),
	"project": runner(func(in investmentGrowing) (any, error) {
		return Project(in.Investment, in.Growth, in.ExitPE), nil
	}),
	"exitPeNeeded": runner(func(in investmentGrowing) (any, error) { return ExitPENeeded(in.Investment, in.Growth), nil }),
	"tone":         runner(func(in struct{ Diff float64 }) (any, error) { return ToneOf(in.Diff), nil }),
	"verdict":      runner(func(in struct{ Gap float64 }) (any, error) { return VerdictOf(in.Gap), nil }),
	"niceStep": runner(func(in struct{ Range, Target float64 }) (any, error) {
		return NiceStep(in.Range, in.Target), nil
	}),
	"quarterSeries": runner(func(in struct{ Quarters []Quarter }) (any, error) { return QuarterSeries(in.Quarters), nil }),
	"weightedTrend": runner(func(in struct {
		Quarters []Quarter
		At       int
	},
	) (any, error) {
		return WeightedTrend(in.Quarters, in.At), nil
	}),
	"trailingGrowth": runner(func(in struct {
		Quarters []Quarter
		Months   int
	},
	) (any, error) {
		return TrailingGrowth(in.Quarters, in.Months), nil
	}),
	"trajectoryFill": runner(func(in struct {
		Quarters []Quarter
		Years    int
	},
	) (any, error) {
		return TrajectoryFill(in.Quarters, in.Years), nil
	}),
	"yearRates": runner(func(in struct {
		Custom CustomGrowth
		Years  int
	},
	) (any, error) {
		return YearRates(in.Custom, in.Years), nil
	}),
	"yearlyAverage": runner(func(in struct{ Rates []float64 }) (any, error) { return YearlyAverage(in.Rates), nil }),
	"baselinePath": runner(func(in struct {
		EPS         float64 `json:"eps"`
		Growth      float64
		Quarters    int
		YearlyRates []float64
	},
	) (any, error) {
		return BaselinePath(in.EPS, in.Growth, in.Quarters, in.YearlyRates), nil
	}),
	"growthScale": runner(func(in struct{ Values []*float64 }) (any, error) { return GrowthScale(in.Values), nil }),
	"typicalPe": runner(func(in struct {
		Quarters []Quarter
		Price    float64
		EPS      float64 `json:"eps"`
		Months   int
	},
	) (any, error) {
		return TypicalPE(in.Quarters, in.Price, in.EPS, in.Months), nil
	}),
	"estimatedExitPe": runner(func(in struct {
		Price         float64
		EPS           float64 `json:"eps"`
		Years         int
		WeightedTrend *float64
		TypicalPE     *float64 `json:"typicalPe"`
	},
	) (any, error) {
		return EstimatedExitPE(in.Price, in.EPS, in.Years, in.WeightedTrend, in.TypicalPE), nil
	}),
	"returnGrid": runner(func(in GridInput) (any, error) { return ReturnGrid(in), nil }),
	"earningsPaths": runner(func(in PathsInput) (any, error) {
		return EarningsPaths(in), nil
	}),
	"howDemanding": runner(func(in struct {
		Investment
		Quarters []Quarter
		ExitPE   Number `json:"exitPe"`
	},
	) (any, error) {
		return HowDemanding(in.Quarters, in.Investment, in.ExitPE), nil
	}),
	"defaultScenarios": runner(func(in struct {
		WeightedTrend *float64
		ExitPE        *float64 `json:"exitPe"`
	},
	) (any, error) {
		return DefaultScenarios(in.WeightedTrend, in.ExitPE), nil
	}),
	"scenarios": runner(func(in struct {
		Investment
		Cases []ScenarioCase
	},
	) (any, error) {
		return Scenarios(in.Investment, in.Cases), nil
	}),
	"shareCountEvents": runner(func(in struct{ Quarters []Quarter }) (any, error) {
		return ShareCountEvents(in.Quarters), nil
	}),
	"correlation": runner(func(in struct{ Xs, Ys []float64 }) (any, error) { return Correlation(in.Xs, in.Ys), nil }),
	"peBands":     runner(func(in struct{ Points []PricePoint }) (any, error) { return PEBands(in.Points), nil }),
	"priceChart": runner(func(in struct {
		Daily    []PricePoint
		Quarters []Quarter
		Range    ChartRange
	},
	) (any, error) {
		return PriceChart(in.Daily, in.Quarters, in.Range), nil
	}),
	"financials":   runner(func(in struct{ Quarters []Quarter }) (any, error) { return Financials(in.Quarters), nil }),
	"compareScale": runner(func(in struct{ Values []*float64 }) (any, error) { return CompareScale(in.Values), nil }),
	"evaluate": runner(func(in struct {
		History History
		Levers  Levers
	},
	) (any, error) {
		return Evaluate(in.History, in.Levers)
	}),
}

func loadCases(t *testing.T) caseFile {
	t.Helper()
	raw, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var f caseFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode cases: %v", err)
	}
	if f.Version != 1 {
		t.Fatalf("cases version = %d, want 1", f.Version)
	}
	return f
}

// TestSharedCases runs every case of testdata/engine/cases.json.
func TestSharedCases(t *testing.T) {
	f := loadCases(t)
	for _, c := range f.Cases {
		t.Run(c.Fn+"/"+c.Name, func(t *testing.T) {
			run, ok := runners[c.Fn]
			if !ok {
				t.Fatalf("no Go runner for fn %q", c.Fn)
			}
			out, err := run(c.Input)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			raw, err := json.Marshal(out)
			if err != nil {
				t.Fatalf("marshal output: %v", err)
			}
			var got, want any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if err := json.Unmarshal(c.Expected, &want); err != nil {
				t.Fatalf("decode expected: %v", err)
			}
			if c.Select != "" {
				obj, ok := got.(map[string]any)
				if !ok {
					t.Fatalf("select %q on a non-object output", c.Select)
				}
				got = obj[c.Select]
			}
			var diffs []string
			compareJSON("$", want, got, c.Tolerance, &diffs)
			if len(diffs) > 0 {
				if len(diffs) > 10 {
					diffs = append(diffs[:10], fmt.Sprintf("... and %d more", len(diffs)-10))
				}
				t.Errorf("output differs from the oracle (note: %s):\n%s", c.Note, joinLines(diffs))
			}
		})
	}
}

// TestSharedCasesCoverEveryRunner fails when a runner has no case, so that
// a new engine function cannot ship without shared cases.
func TestSharedCasesCoverEveryRunner(t *testing.T) {
	f := loadCases(t)
	seen := map[string]bool{}
	for _, c := range f.Cases {
		seen[c.Fn] = true
	}
	var missing []string
	for fn := range runners {
		if !seen[fn] {
			missing = append(missing, fn)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("runners without shared cases: %v", missing)
	}
}

// compareJSON compares decoded JSON values: numbers within tol, null only
// with null, objects with the same keys, arrays of the same length.
func compareJSON(path string, want, got any, tol float64, diffs *[]string) {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		if !ok || math.Abs(w-g) > tol {
			*diffs = append(*diffs, fmt.Sprintf("%s: want %v, got %v", path, w, got))
		}
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("%s: want an object, got %v", path, got))
			return
		}
		keys := make([]string, 0, len(w)+len(g))
		for k := range w {
			keys = append(keys, k)
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			wv, wok := w[k]
			gv, gok := g[k]
			switch {
			case !wok:
				*diffs = append(*diffs, fmt.Sprintf("%s.%s: unexpected field with %v", path, k, gv))
			case !gok:
				*diffs = append(*diffs, fmt.Sprintf("%s.%s: missing, want %v", path, k, wv))
			default:
				compareJSON(path+"."+k, wv, gv, tol, diffs)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			*diffs = append(*diffs, fmt.Sprintf("%s: want an array of %d, got %v", path, len(w), got))
			return
		}
		for i := range w {
			compareJSON(fmt.Sprintf("%s[%d]", path, i), w[i], g[i], tol, diffs)
		}
	default:
		if want != got {
			*diffs = append(*diffs, fmt.Sprintf("%s: want %v, got %v", path, want, got))
		}
	}
}

func joinLines(lines []string) string {
	s := ""
	for _, l := range lines {
		s += "  " + l + "\n"
	}
	return s
}
