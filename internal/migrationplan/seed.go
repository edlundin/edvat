package migrationplan

import (
	"context"

	"github.com/edlundin/edvat/internal/baseatlas"
	"github.com/edlundin/edvat/internal/project"
	"github.com/edlundin/edvat/internal/seed"

	"ariga.io/atlas/sql/schema"
)

func loadSeedStatements(ctx context.Context, cfg project.EnvConfig, desired *schema.Realm, devURL string) ([]baseatlas.Statement, error) {
	var currentRows seed.CurrentRows
	if devURL != "" {
		currentRows = func(ctx context.Context, table string, columns []string) ([]seed.Row, error) {
			return seed.InspectRowsURL(ctx, devURL, table, columns)
		}
	}
	return seed.Plan(ctx, seed.PlanConfig{
		SchemaPaths: cfg.SchemaPaths,
		SQLPaths:    cfg.SeedPaths,
		Mode:        cfg.SeedMode,
		Desired:     desired,
		CurrentRows: currentRows,
	})
}
