package fundamentals

import (
	"github.com/sourcehawk/pricedin/server/internal/sec"
)

// conceptKind says how a split changes a concept's values.
type conceptKind int

const (
	kindMoney    conceptKind = iota // not changed by a split
	kindPerShare                    // divided by the split ratio
	kindShares                      // multiplied by the split ratio
)

func kindOf(concept string) conceptKind {
	switch sec.Concept(concept) {
	case sec.EarningsPerShareDiluted, sec.CommonStockDividendsPerShareDeclared, sec.CommonStockDividendsPerShareCashPaid:
		return kindPerShare
	case sec.WeightedAverageNumberOfDilutedSharesOutstanding:
		return kindShares
	default:
		return kindMoney
	}
}

// unitOf returns the only unit that Derive reads for concept. Values in
// other units, such as the few EPS values that some filers tag as "pure",
// are left out.
func unitOf(concept string) string {
	switch kindOf(concept) {
	case kindPerShare:
		return "USD/shares"
	case kindShares:
		return "shares"
	default:
		return "USD"
	}
}

// conceptsOf returns the concepts that give m, in priority order: for each
// quarter, the first concept that gives a value wins. It returns nil for
// the metrics that are computed from other metrics.
func conceptsOf(m Metric) []sec.Concept {
	switch m {
	case EPSDiluted:
		return []sec.Concept{sec.EarningsPerShareDiluted}
	case Revenue:
		// Revenues comes first because it is the total revenue line. The
		// contract-revenue concepts (ASC 606) leave out revenue from other
		// sources, and some filers tag only that part with them: ADM's
		// 10-Qs from November 2024 give about $6 billion of contract revenue
		// next to about $21 billion of Revenues for the same quarter.
		return []sec.Concept{
			sec.Revenues,
			sec.RevenueFromContractWithCustomerExcludingAssessedTax,
			sec.RevenueFromContractWithCustomerIncludingAssessedTax,
			sec.SalesRevenueNet,
			sec.SalesRevenueGoodsNet,
		}
	case OperatingIncome:
		return []sec.Concept{sec.OperatingIncomeLoss}
	case OperatingCashFlow:
		return []sec.Concept{
			sec.NetCashProvidedByUsedInOperatingActivities,
			sec.NetCashProvidedByUsedInOperatingActivitiesContinuingOperations,
		}
	case CapitalExpenditure:
		return []sec.Concept{
			sec.PaymentsToAcquirePropertyPlantAndEquipment,
			sec.PaymentsToAcquireProductiveAssets,
		}
	case DilutedShares:
		return []sec.Concept{sec.WeightedAverageNumberOfDilutedSharesOutstanding}
	case DividendsPerShare:
		return []sec.Concept{sec.CommonStockDividendsPerShareDeclared, sec.CommonStockDividendsPerShareCashPaid}
	case NetIncome:
		return []sec.Concept{sec.NetIncomeLoss}
	default:
		return nil
	}
}

// isMetricConcept reports whether concept feeds a metric.
func isMetricConcept(concept string) bool {
	for _, m := range Metrics() {
		for _, c := range conceptsOf(m) {
			if string(c) == concept {
				return true
			}
		}
	}
	return false
}

// isPeriodicReport reports whether values of form may feed quarters. A
// 10-KT is the annual report for a transition period after a change of the
// fiscal year end. It is audited like a 10-K, and its periods never match a
// quarter of the new calendar except as reported comparatives. Current
// reports (8-K), proxies and other forms are left out.
func isPeriodicReport(form string) bool {
	switch form {
	case "10-Q", "10-Q/A", "10-K", "10-K/A", "10-KT", "10-KT/A":
		return true
	default:
		return false
	}
}

// isAnnualReport reports whether form is a 10-K or a 10-KT, with or
// without amendment. Only these define fiscal years: a 10-Q can report a
// trailing twelve months that is not a fiscal year.
func isAnnualReport(form string) bool {
	switch form {
	case "10-K", "10-K/A", "10-KT", "10-KT/A":
		return true
	default:
		return false
	}
}
