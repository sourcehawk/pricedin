package fundamentals

import (
	"fmt"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Metric names a quarterly value. The names are stable: storage and the API
// use them as keys.
type Metric string

// The derived metrics. Money is in US dollars, per-share values in US
// dollars per share, and DilutedShares in whole shares.
const (
	EPSDiluted         Metric = "eps_diluted"
	Revenue            Metric = "revenue"
	OperatingIncome    Metric = "operating_income"
	OperatingCosts     Metric = "operating_costs"
	OperatingCashFlow  Metric = "operating_cash_flow"
	CapitalExpenditure Metric = "capital_expenditure"
	FreeCashFlow       Metric = "free_cash_flow"
	DilutedShares      Metric = "diluted_shares"
	DividendsPerShare  Metric = "dividends_per_share"
	NetIncome          Metric = "net_income"
)

// Metrics returns every metric in a fixed order. The caller owns the slice.
func Metrics() []Metric {
	return []Metric{
		EPSDiluted, Revenue, OperatingIncome, OperatingCosts, OperatingCashFlow,
		CapitalExpenditure, FreeCashFlow, DilutedShares, DividendsPerShare, NetIncome,
	}
}

// Method says how a value was obtained.
type Method string

// The methods.
const (
	// MethodReported is a three-month value as a filing reports it.
	MethodReported Method = "reported"
	// MethodYTDDifference is Q2 = H1 − Q1 or Q3 = 9M − H1.
	MethodYTDDifference Method = "ytd_difference"
	// MethodFYDifference is Q4 = full year − 9M.
	MethodFYDifference Method = "fy_difference"
	// MethodWeightedAverageQ4 is the Q4 diluted share count that makes the
	// duration-weighted quarters match the full-year average. It is an
	// estimate.
	MethodWeightedAverageQ4 Method = "weighted_average_q4"
	// MethodDerived is a value computed from other metrics of the same
	// quarter: free cash flow and operating costs.
	MethodDerived Method = "derived"
	// MethodTTM is the sum of four consecutive quarters (see TTM).
	MethodTTM Method = "ttm"
)

// Value is one derived number with its sources.
type Value struct {
	V      float64
	Method Method
	// Estimate is true when the value is not exact, such as a Q4 share
	// count from the weighted-average rule.
	Estimate bool
	// Inputs are the reported facts the value comes from, never empty.
	Inputs []SourceRef
	// Filed is the latest filing date among Inputs.
	Filed domain.Date
	// FirstFiled is the date on which the filings first gave the quarter,
	// also through inputs that are not in Inputs. Later filings repeat
	// earlier periods as comparatives, so FirstFiled is usually much
	// earlier than Filed.
	FirstFiled domain.Date
}

// SourceRef is one reported fact that a value uses.
type SourceRef struct {
	Accession string
	Form      string
	Concept   string
	Filed     domain.Date
	// PeriodStart is nil for an instant fact. The period is as filed, and
	// it can start or end up to three days away from the quarter's.
	PeriodStart *domain.Date
	PeriodEnd   domain.Date
	// Value is the number as the filing reports it, before split
	// adjustment.
	Value float64
	// SplitRatio is the product of the ratios of the confirmed splits that
	// were applied to Value: per-share values were divided by it, share
	// counts multiplied. It is 1 when no split applied.
	SplitRatio float64
	// Scale is the factor that corrected a share count tagged in
	// thousands or millions (see ConflictShareScale), 1 when none.
	Scale float64
}

// Quarter is one fiscal quarter.
type Quarter struct {
	FiscalYear    int // as the company names its fiscal years, such as 2027
	FiscalQuarter int // 1 to 4
	// Label is "Q<n> FY<yy>", such as "Q2 FY27".
	Label                  string
	PeriodStart, PeriodEnd domain.Date
	// Values holds the metrics that could be derived. A missing key means
	// that the filings do not give the metric for this quarter.
	Values map[Metric]Value
}

// Series is the derived history of one company.
type Series struct {
	// Quarters are consecutive fiscal quarters, oldest first. They start
	// after the last break in the fiscal calendar, such as a change of the
	// fiscal year end or a year whose quarters the filings do not bound.
	Quarters []Quarter
	// Splits are the confirmed splits, oldest first: the provider events,
	// and the SEC split-ratio evidence with no provider event near its date.
	Splits []domain.Split
	// Conflicts are the reconciliation problems that the data page lists.
	Conflicts []Conflict
}

// ConflictKind classifies a Conflict.
type ConflictKind string

// The conflict kinds.
const (
	// ConflictUnconfirmedSplit is a per-share value or share count that a
	// later filing restated at a clean split ratio, with no confirmed split
	// between the two filings. The restated value is used as filed.
	ConflictUnconfirmedSplit ConflictKind = "unconfirmed_split"
	// ConflictSplitMismatch is a restatement across a confirmed split that
	// the split ratio does not explain.
	ConflictSplitMismatch ConflictKind = "split_mismatch"
	// ConflictShareScale is a share count on a scale that does not match
	// another filing or a reference value, for example 1,000 or 1,000,000
	// times off. When two filings of one period disagree by that factor,
	// the larger value is used. When a used count is rescaled, Original is
	// zero and Observed is the factor that multiplied it, 1,000 or
	// 1,000,000. When an input or the result of a Q4 estimate differs in
	// scale from the full year, Q4 gets no estimated count: Restated is the
	// full year and Observed the input or result over it.
	ConflictShareScale ConflictKind = "share_scale"
	// ConflictSplitEvidence is a split that SEC data gives and that no
	// provider event matches, although the provider was checked. When a
	// provider event of another ratio lies near its date, only the provider
	// event is applied. Otherwise the SEC split is applied too.
	ConflictSplitEvidence ConflictKind = "split_evidence"
	// ConflictSplitDate is an SEC split that the filings date only to a
	// window, with a filing inside the window that neither repeats nor
	// restates an earlier per-share value. The split is dated before that
	// filing, so its values are used as filed. PeriodStart and PeriodEnd
	// give the window, Restated the filing, and Split the split.
	ConflictSplitDate ConflictKind = "split_date"
)

// Conflict is one reconciliation problem.
type Conflict struct {
	Kind        ConflictKind
	Concept     string
	PeriodStart *domain.Date
	PeriodEnd   domain.Date
	// Original and Restated are the first and the latest filing of the
	// value. For ConflictSplitEvidence, Restated is the SEC fact that gives
	// the split and Original is zero.
	Original, Restated SourceRef
	// Observed is the ratio between Original and Restated in the direction
	// of a split (old per-share value / new, or new share count / old).
	// Expected is the ratio of the confirmed splits between the two
	// filings, 1 when there are none. Both are 0 for ConflictSplitEvidence.
	Observed, Expected float64
	// Split is the unmatched split for ConflictSplitEvidence, else nil.
	Split *domain.Split
}

// Options configure Derive.
type Options struct {
	// AsOf, when set, gives the point-in-time view: facts filed after it
	// and splits dated after it are ignored.
	AsOf *domain.Date
	// Splits are confirmed split events from the price provider. They may
	// be empty.
	Splits []domain.Split
	// ProviderChecked says that the provider was asked for splits, also
	// when it has none. Then an SEC split that no provider event matches
	// gives a ConflictSplitEvidence, as it does when Splits is not empty.
	ProviderChecked bool
}

func quarterLabel(fiscalYear, fiscalQuarter int) string {
	return fmt.Sprintf("Q%d FY%02d", fiscalQuarter, fiscalYear%100)
}
