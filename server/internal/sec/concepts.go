package sec

// Concept is an XBRL concept that PricedIn keeps from SEC company facts. Its
// value is the concept name as SEC writes it, which is also domain.Fact's
// Concept.
type Concept string

// The kept concepts. All are in the us-gaap taxonomy, except
// EntityCommonStockSharesOutstanding, which is in dei.
const (
	EarningsPerShareDiluted                                        Concept = "EarningsPerShareDiluted"
	Revenues                                                       Concept = "Revenues"
	RevenueFromContractWithCustomerExcludingAssessedTax            Concept = "RevenueFromContractWithCustomerExcludingAssessedTax"
	RevenueFromContractWithCustomerIncludingAssessedTax            Concept = "RevenueFromContractWithCustomerIncludingAssessedTax"
	SalesRevenueNet                                                Concept = "SalesRevenueNet"
	SalesRevenueGoodsNet                                           Concept = "SalesRevenueGoodsNet"
	OperatingIncomeLoss                                            Concept = "OperatingIncomeLoss"
	NetCashProvidedByUsedInOperatingActivities                     Concept = "NetCashProvidedByUsedInOperatingActivities"
	NetCashProvidedByUsedInOperatingActivitiesContinuingOperations Concept = "NetCashProvidedByUsedInOperatingActivitiesContinuingOperations"
	PaymentsToAcquirePropertyPlantAndEquipment                     Concept = "PaymentsToAcquirePropertyPlantAndEquipment"
	PaymentsToAcquireProductiveAssets                              Concept = "PaymentsToAcquireProductiveAssets"
	WeightedAverageNumberOfDilutedSharesOutstanding                Concept = "WeightedAverageNumberOfDilutedSharesOutstanding"
	CommonStockDividendsPerShareDeclared                           Concept = "CommonStockDividendsPerShareDeclared"
	CommonStockDividendsPerShareCashPaid                           Concept = "CommonStockDividendsPerShareCashPaid"
	StockholdersEquityNoteStockSplitConversionRatio1               Concept = "StockholdersEquityNoteStockSplitConversionRatio1"
	NetIncomeLoss                                                  Concept = "NetIncomeLoss"
	EntityCommonStockSharesOutstanding                             Concept = "EntityCommonStockSharesOutstanding"
)

// The taxonomies of the kept concepts.
const (
	TaxonomyUSGAAP = "us-gaap"
	TaxonomyDEI    = "dei"
)

// Concepts returns every kept concept, in a fixed order. The caller owns the
// slice.
func Concepts() []Concept {
	return []Concept{
		EarningsPerShareDiluted,
		Revenues,
		RevenueFromContractWithCustomerExcludingAssessedTax,
		RevenueFromContractWithCustomerIncludingAssessedTax,
		SalesRevenueNet,
		SalesRevenueGoodsNet,
		OperatingIncomeLoss,
		NetCashProvidedByUsedInOperatingActivities,
		NetCashProvidedByUsedInOperatingActivitiesContinuingOperations,
		PaymentsToAcquirePropertyPlantAndEquipment,
		PaymentsToAcquireProductiveAssets,
		WeightedAverageNumberOfDilutedSharesOutstanding,
		CommonStockDividendsPerShareDeclared,
		CommonStockDividendsPerShareCashPaid,
		StockholdersEquityNoteStockSplitConversionRatio1,
		NetIncomeLoss,
		EntityCommonStockSharesOutstanding,
	}
}

// Taxonomy returns the taxonomy that holds c: TaxonomyDEI or TaxonomyUSGAAP.
func (c Concept) Taxonomy() string {
	if c == EntityCommonStockSharesOutstanding {
		return TaxonomyDEI
	}
	return TaxonomyUSGAAP
}
