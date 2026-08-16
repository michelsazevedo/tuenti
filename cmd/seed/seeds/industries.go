package seeds

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/michelsazevedo/tuenti/internal/infrastructure/database"
)

var industryNames = []string{
	"Agriculture",
	"Advocacy",
	"Automotive",
	"Construction",
	"Consulting",
	"Education",
	"Energy",
	"Engineering",
	"Entertainment",
	"Financial Services",
	"Food & Beverage",
	"Government",
	"Healthcare",
	"Hospitality",
	"Information Technology",
	"Insurance",
	"Legal",
	"Manufacturing",
	"Marketing & Advertising",
	"Media",
	"Nonprofit",
	"Real Estate",
	"Retail",
	"Telecommunications",
	"Transportation & Logistics",
	"Travel & Tourism",
	"Other",
}

const upsertIndustries = `INSERT INTO industries (name, slug)
SELECT name, slug FROM unnest($1::text[], $2::text[]) AS t(name, slug)
ON CONFLICT (name) DO UPDATE SET slug = EXCLUDED.slug, updated_at = now()`

type IndustriesSeed struct {
	db database.DBTX
}

func NewIndustriesSeed(db database.DBTX) *IndustriesSeed {
	return &IndustriesSeed{db: db}
}

func industrySlug(name string) string {
	cleaned := strings.ReplaceAll(name, "&", "")

	return strings.ToLower(strings.Join(strings.Fields(cleaned), "_"))
}

func (s *IndustriesSeed) Run(ctx context.Context) error {
	slugs := make([]string, len(industryNames))
	for i, name := range industryNames {
		slugs[i] = industrySlug(name)
	}

	tag, err := s.db.Exec(ctx, upsertIndustries, industryNames, slugs)
	if err != nil {
		return fmt.Errorf("seed industries: %w", err)
	}

	log.Info().
		Str("event", "industries_seeded").
		Int64("rows", tag.RowsAffected()).
		Msg("industries upserted")

	return nil
}
