package migrationplan

import (
	"context"
	"fmt"
	"strings"

	"github.com/edlundin/edvat/internal/baseatlas"
	"github.com/edlundin/edvat/internal/project"
)

type Options struct {
	Name             string
	DevURL           string
	ManageRoles      bool
	ManageUsers      bool
	AllowDestructive bool
}

type Plan struct {
	Statements []baseatlas.Statement
	Findings   []Finding
}

type Finding struct {
	Kind    string
	Message string
}

type DestructiveError struct {
	Findings []Finding
}

func (e DestructiveError) Error() string {
	messages := make([]string, 0, len(e.Findings))
	for _, finding := range e.Findings {
		messages = append(messages, finding.Message)
	}
	return "migration contains destructive statements; rerun with --allow-destructive after review: " + strings.Join(messages, "; ")
}

func Build(ctx context.Context, cfg project.EnvConfig, opts Options) (Plan, error) {
	engine := baseatlas.New()
	desiredState, err := loadDesiredMigrationState(ctx, engine, cfg.SchemaPaths)
	if err != nil {
		return Plan{}, err
	}
	if len(desiredState.Roles) > 0 && !opts.ManageRoles {
		return Plan{}, fmt.Errorf("role blocks require --manage-roles")
	}
	if len(desiredState.Users) > 0 && !opts.ManageUsers {
		return Plan{}, fmt.Errorf("user blocks require --manage-users")
	}

	currentState := migrationState{}
	if opts.DevURL != "" {
		currentState, err = inspectCurrentMigrationState(ctx, engine, opts.DevURL, opts.ManageRoles, opts.ManageUsers)
		if err != nil {
			return Plan{}, err
		}
	}
	changes, err := engine.Diff(ctx, currentState.Realm, desiredState.Realm)
	if err != nil {
		return Plan{}, err
	}
	changes = inferColumnRenames(changes)
	statements, err := engine.PlanSQL(ctx, opts.Name, changes)
	if err != nil {
		return Plan{}, err
	}
	statements = appendPgExtStatements(statements, currentState, desiredState, opts.ManageRoles, opts.ManageUsers)
	seedStatements, err := loadSeedStatements(ctx, cfg, desiredState.Realm, opts.DevURL)
	if err != nil {
		return Plan{}, err
	}
	statements = append(statements, seedStatements...)
	statements = suppressManagedExclusionConstraintDrops(statements, desiredState.Exclusions)
	statements = suppressDropIndexesMentionedInHCL(statements, cfg.SchemaPaths)
	statements = orderMigrationStatements(statements)

	findings := destructiveFindings(statements)
	return Plan{Statements: statements, Findings: findings}, nil
}
