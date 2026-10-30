package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sourcehawk/pricedin/server/internal/domain"
)

var companyStage = stage{
	name: "stage_companies",
	ddl: `ord int, cik bigint, name text, sic text, sic_description text,
		fiscal_year_end text, state_of_incorporation text`,
	columns: []string{"ord", "cik", "name", "sic", "sic_description", "fiscal_year_end", "state_of_incorporation"},
}

// UpsertCompanies inserts new companies and updates the ones whose fields
// changed. It returns the number of rows inserted or changed. When a batch
// holds a CIK twice, the later copy wins.
func (s *Store) UpsertCompanies(ctx context.Context, companies []domain.Company) (int64, error) {
	rows := make([][]any, len(companies))
	for i, c := range companies {
		rows[i] = []any{i, int64(c.CIK), c.Name, c.SIC, c.SICDescription, c.FiscalYearEnd, c.StateOfIncorporation}
	}
	n, err := s.copyInsert(ctx, companyStage, rows, `
		INSERT INTO companies (cik, name, sic, sic_description, fiscal_year_end, state_of_incorporation)
		SELECT DISTINCT ON (cik) cik, name, sic, sic_description, fiscal_year_end, state_of_incorporation
		FROM stage_companies
		ORDER BY cik, ord DESC
		ON CONFLICT (cik) DO UPDATE SET
			name = excluded.name,
			sic = excluded.sic,
			sic_description = excluded.sic_description,
			fiscal_year_end = excluded.fiscal_year_end,
			state_of_incorporation = excluded.state_of_incorporation,
			updated_at = now()
		WHERE (companies.name, companies.sic, companies.sic_description,
			   companies.fiscal_year_end, companies.state_of_incorporation)
			IS DISTINCT FROM
			  (excluded.name, excluded.sic, excluded.sic_description,
			   excluded.fiscal_year_end, excluded.state_of_incorporation)`)
	if err != nil {
		return 0, fmt.Errorf("upsert companies: %w", err)
	}
	return n, nil
}

const companyColumns = `c.cik, c.name, c.sic, c.sic_description, c.fiscal_year_end, c.state_of_incorporation`

func scanCompany(row pgx.Row) (domain.Company, error) {
	var c domain.Company
	var cik int64
	err := row.Scan(&cik, &c.Name, &c.SIC, &c.SICDescription, &c.FiscalYearEnd, &c.StateOfIncorporation)
	c.CIK = domain.CIK(cik)
	return c, err
}

// GetCompany returns the company with the CIK, or ErrNotFound.
func (s *Store) GetCompany(ctx context.Context, cik domain.CIK) (domain.Company, error) {
	c, err := scanCompany(s.db.QueryRow(ctx,
		`SELECT `+companyColumns+` FROM companies c WHERE c.cik = $1`, int64(cik)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Company{}, notFound("get company %s", cik)
	}
	if err != nil {
		return domain.Company{}, fmt.Errorf("get company %s: %w", cik, err)
	}
	return c, nil
}

// GetCompanyBySymbol returns the company that the symbol maps to now, as
// GetCurrentTicker finds it, or ErrNotFound.
func (s *Store) GetCompanyBySymbol(ctx context.Context, symbol string) (domain.Company, error) {
	t, err := s.GetCurrentTicker(ctx, symbol)
	if err != nil {
		return domain.Company{}, err
	}
	return s.GetCompany(ctx, t.CIK)
}
