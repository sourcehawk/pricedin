package engine

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestDefaultScenarios(t *testing.T) {
	opt := cmpopts.EquateApprox(0, 1e-12)
	want := []ScenarioCase{
		{Name: "Bear", Growth: 0.1, ExitPE: 15},
		{Name: "Base", Growth: 0.16, ExitPE: 20},
		{Name: "Bull", Growth: 0.22, ExitPE: 24},
	}
	if diff := cmp.Diff(want, DefaultScenarios(ptr(0.1595), ptr(20.0)), opt); diff != "" {
		t.Errorf("cases around 15.95%% at 20x (-want +got):\n%s", diff)
	}
	fallback := []ScenarioCase{
		{Name: "Bear", Growth: -0.01, ExitPE: 15},
		{Name: "Base", Growth: 0.05, ExitPE: 20},
		{Name: "Bull", Growth: 0.11, ExitPE: 24},
	}
	if diff := cmp.Diff(fallback, DefaultScenarios(nil, nil), opt); diff != "" {
		t.Errorf("cases without a trend or exit P/E (-want +got):\n%s", diff)
	}
	if got := DefaultScenarios(ptr(0.0), ptr(7.0))[0].ExitPE; got != 6 {
		t.Errorf("bear exit P/E = %v, want at least 6", got)
	}
}

// The cases that meet the target are named by their own results, whatever
// their order.
func TestScenariosNameTheCasesThatMeetTheTarget(t *testing.T) {
	set := Scenarios(grmn, []ScenarioCase{
		{Name: "Bear", Growth: 0.3, ExitPE: 30},
		{Name: "Base", Growth: 0.05, ExitPE: 15},
		{Name: "Bull", Growth: 0.1, ExitPE: 18},
	})
	if diff := cmp.Diff([]string{"Bear"}, set.MeetingTarget); diff != "" {
		t.Errorf("meeting target (-want +got):\n%s", diff)
	}
	base := wantSome(t, "base", set.Cases[1].Result)
	near(t, "worst return", wantSome(t, "worst", set.WorstYearlyReturn), base.YearlyReturn, 0)
	p := Project(grmn, 0.05, 15)
	near(t, "base yearly return", base.YearlyReturn, p.YearlyReturn, 0)
	if base.MeetsTarget || base.Tone != ToneBad {
		t.Errorf("base = %+v, want a miss", base)
	}
}

func TestScenariosNeedPositiveEPS(t *testing.T) {
	inv := grmn
	inv.EPS = 0
	set := Scenarios(inv, DefaultScenarios(nil, nil))
	for _, c := range set.Cases {
		wantNone(t, c.Name, c.Result, ReasonNeedsPositiveEPS)
	}
	wantNone(t, "worst", set.WorstYearlyReturn, ReasonNeedsPositiveEPS)
	if set.MeetingTarget == nil || len(set.MeetingTarget) != 0 {
		t.Errorf("meeting target = %#v, want an empty list", set.MeetingTarget)
	}
}
