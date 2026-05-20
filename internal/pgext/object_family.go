package pgext

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
)

// stateIDs returns deterministic object IDs from desired first, plus current-only IDs.
// Most pgext object families diff a current/desired map by object identity; keeping
// the traversal shared avoids subtle ordering drift as new families are added.
func stateIDs[C ~map[string]V, V any](current, desired C) []string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(current)+len(desired))
	for id := range desired {
		ids = append(ids, id)
		seen[id] = true
	}
	for id := range current {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func parseStateFiles[S ~map[string]V, V any](paths []string, family string, parse func([]byte, string) (S, error)) (S, error) {
	state := make(S)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s source %q: %w", family, path, err)
		}
		parsed, err := parse(src, path)
		if err != nil {
			return nil, err
		}
		for id, value := range parsed {
			state[id] = value
		}
	}
	return state, nil
}

func inspectURL[S any](ctx context.Context, url, family string, inspect func(context.Context, *sql.DB) (S, error)) (S, error) {
	var zero S
	db, err := sql.Open("postgres", url)
	if err != nil {
		return zero, fmt.Errorf("open postgres database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return zero, fmt.Errorf("ping postgres database: %w", err)
	}
	state, err := inspect(ctx, db)
	if err != nil {
		return zero, fmt.Errorf("inspect postgres %s: %w", family, err)
	}
	return state, nil
}
