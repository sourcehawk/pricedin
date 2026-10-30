package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
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
		if bad := unknownKeys(raw, reflect.TypeFor[In](), "$"); len(bad) > 0 {
			return nil, fmt.Errorf("input keys that match no JSON field name exactly: %v", bad)
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
	"tone": runner(func(in struct {
		Diff float64 `json:"diff"`
	},
	) (any, error) {
		return ToneOf(in.Diff), nil
	}),
	"verdict": runner(func(in struct {
		Gap float64 `json:"gap"`
	},
	) (any, error) {
		return VerdictOf(in.Gap), nil
	}),
	"niceStep": runner(func(in struct {
		Range  float64 `json:"range"`
		Target float64 `json:"target"`
	},
	) (any, error) {
		return NiceStep(in.Range, in.Target), nil
	}),
	"quarterSeries": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
	},
	) (any, error) {
		return QuarterSeries(in.Quarters), nil
	}),
	"weightedTrend": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
		At       int       `json:"at"`
	},
	) (any, error) {
		return WeightedTrend(in.Quarters, in.At), nil
	}),
	"trailingGrowth": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
		Months   int       `json:"months"`
	},
	) (any, error) {
		return TrailingGrowth(in.Quarters, in.Months), nil
	}),
	"trajectoryFill": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
		Years    int       `json:"years"`
	},
	) (any, error) {
		return TrajectoryFill(in.Quarters, in.Years), nil
	}),
	"yearRates": runner(func(in struct {
		Custom CustomGrowth `json:"custom"`
		Years  int          `json:"years"`
	},
	) (any, error) {
		return YearRates(in.Custom, in.Years), nil
	}),
	"yearlyAverage": runner(func(in struct {
		Rates []float64 `json:"rates"`
	},
	) (any, error) {
		return YearlyAverage(in.Rates), nil
	}),
	"baselinePath": runner(func(in struct {
		EPS         float64   `json:"eps"`
		Growth      float64   `json:"growth"`
		Quarters    int       `json:"quarters"`
		YearlyRates []float64 `json:"yearlyRates"`
	},
	) (any, error) {
		return BaselinePath(in.EPS, in.Growth, in.Quarters, in.YearlyRates), nil
	}),
	"growthScale": runner(func(in struct {
		Values []*float64 `json:"values"`
	},
	) (any, error) {
		return GrowthScale(in.Values), nil
	}),
	"typicalPe": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
		Price    float64   `json:"price"`
		EPS      float64   `json:"eps"`
		Months   int       `json:"months"`
	},
	) (any, error) {
		return TypicalPE(in.Quarters, in.Price, in.EPS, in.Months), nil
	}),
	"estimatedExitPe": runner(func(in struct {
		Price         float64  `json:"price"`
		EPS           float64  `json:"eps"`
		Years         int      `json:"years"`
		WeightedTrend *float64 `json:"weightedTrend"`
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
		Quarters []Quarter `json:"quarters"`
		ExitPE   Number    `json:"exitPe"`
	},
	) (any, error) {
		return HowDemanding(in.Quarters, in.Investment, in.ExitPE), nil
	}),
	"defaultScenarios": runner(func(in struct {
		WeightedTrend *float64 `json:"weightedTrend"`
		ExitPE        *float64 `json:"exitPe"`
	},
	) (any, error) {
		return DefaultScenarios(in.WeightedTrend, in.ExitPE), nil
	}),
	"scenarios": runner(func(in struct {
		Investment
		Cases []ScenarioCase `json:"cases"`
	},
	) (any, error) {
		return Scenarios(in.Investment, in.Cases), nil
	}),
	"shareCountEvents": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
	},
	) (any, error) {
		return ShareCountEvents(in.Quarters), nil
	}),
	"correlation": runner(func(in struct {
		Xs []float64 `json:"xs"`
		Ys []float64 `json:"ys"`
	},
	) (any, error) {
		return Correlation(in.Xs, in.Ys), nil
	}),
	"peBands": runner(func(in struct {
		Points []PricePoint `json:"points"`
	},
	) (any, error) {
		return PEBands(in.Points), nil
	}),
	"priceChart": runner(func(in struct {
		Daily    []PricePoint `json:"daily"`
		Quarters []Quarter    `json:"quarters"`
		Range    ChartRange   `json:"range"`
	},
	) (any, error) {
		return PriceChart(in.Daily, in.Quarters, in.Range), nil
	}),
	"bandPrice": runner(func(in struct {
		TTMEPS   *float64 `json:"ttmEps"`
		Multiple float64  `json:"multiple"`
	},
	) (any, error) {
		return BandPrice(in.TTMEPS, in.Multiple), nil
	}),
	"financials": runner(func(in struct {
		Quarters []Quarter `json:"quarters"`
	},
	) (any, error) {
		return Financials(in.Quarters), nil
	}),
	"compareScale": runner(func(in struct {
		Values []*float64 `json:"values"`
	},
	) (any, error) {
		return CompareScale(in.Values), nil
	}),
	"evaluate": runner(func(in struct {
		History History `json:"history"`
		Levers  Levers  `json:"levers"`
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

// unknownKeys returns the path of each object key in raw that is not the
// exact JSON name of a field of t. encoding/json matches names without
// regard to case; the TypeScript engine does not, so the cases must use the
// exact names.
func unknownKeys(raw json.RawMessage, t reflect.Type, path string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return nil
	}
	var bad []string
	switch t.Kind() {
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		fields := jsonFields(t)
		for k, v := range obj {
			ft, ok := fields[k]
			if !ok {
				bad = append(bad, path+"."+k)
				continue
			}
			bad = append(bad, unknownKeys(v, ft, path+"."+k)...)
		}
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil
		}
		for i, v := range items {
			bad = append(bad, unknownKeys(v, t.Elem(), fmt.Sprintf("%s[%d]", path, i))...)
		}
	case reflect.Map:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		for k, v := range obj {
			bad = append(bad, unknownKeys(v, t.Elem(), path+"."+k)...)
		}
	default:
	}
	return bad
}

// jsonFields maps the exact JSON name of each field of struct t, including
// the fields of embedded structs, to its type.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		switch {
		case name == "-" || (!f.IsExported() && !f.Anonymous):
		case f.Anonymous && tag == "":
			for k, v := range jsonFields(f.Type) {
				out[k] = v
			}
		case name == "":
			out[f.Name] = f.Type
		default:
			out[name] = f.Type
		}
	}
	return out
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

func TestUnknownKeysAreCaseExact(t *testing.T) {
	raw := json.RawMessage(`{"Price": 1, "eps": 2, "nested": [{"Label": "x"}]}`)
	type in struct {
		Price  float64   `json:"price"`
		EPS    float64   `json:"eps"`
		Nested []Quarter `json:"nested"`
	}
	got := unknownKeys(raw, reflect.TypeFor[in](), "$")
	slices.Sort(got)
	if want := []string{"$.Price", "$.nested[0].Label"}; !slices.Equal(got, want) {
		t.Errorf("unknownKeys = %v, want %v", got, want)
	}
}
