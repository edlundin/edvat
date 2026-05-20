package migratedir

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type AppliedMigration struct {
	Path string
	SQL  string
}

func ReplayURL(ctx context.Context, url, dir string) ([]AppliedMigration, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("dev url is required to replay migrations")
	}
	db, err := openReplayDB(ctx, url)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return Replay(ctx, db, dir)
}

func ReplayUnappliedURL(ctx context.Context, url, dir string) ([]AppliedMigration, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("dev url is required to replay migrations")
	}
	db, err := openReplayDB(ctx, url)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	version, err := AppliedVersion(ctx, db)
	if err != nil {
		return nil, err
	}
	return ReplayAfterVersion(ctx, db, dir, version)
}

func openReplayDB(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, fmt.Errorf("open dev database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping dev database: %w", err)
	}
	return db, nil
}

func Replay(ctx context.Context, exec Executor, dir string) ([]AppliedMigration, error) {
	return ReplayAfterVersion(ctx, exec, dir, 0)
}

func ReplayAfterVersion(ctx context.Context, exec Executor, dir string, appliedVersion int64) ([]AppliedMigration, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("migration dir is required")
	}
	files, err := migrationFiles(dir)
	if err != nil {
		return nil, err
	}
	applied := make([]AppliedMigration, 0, len(files))
	for _, file := range files {
		version, err := migrationVersion(file)
		if err != nil {
			return applied, err
		}
		if version <= appliedVersion {
			continue
		}
		if err := ctx.Err(); err != nil {
			return applied, err
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return applied, fmt.Errorf("read migration %q: %w", file, err)
		}
		sqlText := strings.TrimSpace(string(content))
		if sqlText == "" {
			continue
		}
		if _, err := exec.ExecContext(ctx, sqlText); err != nil {
			return applied, fmt.Errorf("apply migration %q: %w", file, err)
		}
		applied = append(applied, AppliedMigration{Path: file, SQL: sqlText})
	}
	return applied, nil
}

func AppliedVersion(ctx context.Context, db Queryer) (int64, error) {
	var version int64
	var dirty bool
	err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		if strings.Contains(err.Error(), `relation "schema_migrations" does not exist`) {
			return 0, nil
		}
		return 0, fmt.Errorf("inspect applied migration version: %w", err)
	}
	if dirty {
		return 0, fmt.Errorf("schema_migrations is dirty at version %d", version)
	}
	return version, nil
}

func migrationVersion(path string) (int64, error) {
	name := filepath.Base(path)
	prefix := strings.SplitN(name, "_", 2)[0]
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse migration version %q: %w", name, err)
	}
	return version, nil
}

func migrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read migration dir %q: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" || strings.HasSuffix(entry.Name(), ".down.sql") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}
