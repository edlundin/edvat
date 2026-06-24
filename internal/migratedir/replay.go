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
	nonEmpty, err := hasUserObjects(ctx, db)
	if err != nil {
		return nil, err
	}
	if version == 0 && nonEmpty {
		return nil, nil
	}
	if version > 0 && !nonEmpty {
		return nil, fmt.Errorf("dev database has schema_migrations version %d but no schema objects", version)
	}
	return ReplayAfterVersion(ctx, db, dir, version)
}

func HasUserObjectsURL(ctx context.Context, url string) (bool, error) {
	if strings.TrimSpace(url) == "" {
		return false, fmt.Errorf("dev url is required")
	}
	db, err := openReplayDB(ctx, url)
	if err != nil {
		return false, err
	}
	defer db.Close()
	return hasUserObjects(ctx, db)
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
	start := 0
	if appliedVersion == 0 {
		start, err = latestInitialMigrationIndex(files)
		if err != nil {
			return nil, err
		}
	}
	applied := make([]AppliedMigration, 0, len(files)-start)
	for i, file := range files {
		if i < start {
			content, err := os.ReadFile(file)
			if err != nil {
				return applied, fmt.Errorf("read migration %q: %w", file, err)
			}
			sqlText := extensionStatementsOnly(normalizeReplaySQL(strings.TrimSpace(string(content))))
			if sqlText != "" {
				if _, err := exec.ExecContext(ctx, sqlText); err != nil {
					return applied, fmt.Errorf("apply migration %q: %w", file, err)
				}
			}
			continue
		}
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
		sqlText := normalizeReplaySQL(strings.TrimSpace(string(content)))
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

func normalizeReplaySQL(sqlText string) string {
	sqlText = strings.ReplaceAll(sqlText, `CREATE SCHEMA "public";`, `CREATE SCHEMA IF NOT EXISTS "public";`)
	parts := splitSQLStatements(sqlText)
	out := parts[:0]
	for _, part := range parts {
		if skipSchemaReplayStatement(part) {
			continue
		}
		out = append(out, part)
	}
	sortReplayStatements(out)
	return strings.Join(out, ";\n")
}

func skipSchemaReplayStatement(statement string) bool {
	sql := statementWithoutLeadingComments(statement)
	return strings.HasPrefix(sql, "UPDATE ") || strings.HasPrefix(sql, "DELETE ")
}

func sortReplayStatements(parts []string) {
	sort.SliceStable(parts, func(i, j int) bool {
		return replayStatementRank(parts[i]) < replayStatementRank(parts[j])
	})
}

func replayStatementRank(statement string) int {
	sql := statementWithoutLeadingComments(statement)
	if strings.HasPrefix(sql, "INSERT INTO \"USERS\"") || strings.HasPrefix(sql, "INSERT INTO USERS") {
		return 10
	}
	if strings.HasPrefix(sql, "INSERT INTO \"USER_IDENTITIES\"") || strings.HasPrefix(sql, "INSERT INTO USER_IDENTITIES") {
		return 11
	}
	if strings.HasPrefix(sql, "INSERT INTO \"DEPLOYMENTS\"") || strings.HasPrefix(sql, "INSERT INTO DEPLOYMENTS") {
		return 12
	}
	if strings.HasPrefix(sql, "INSERT ") || strings.HasPrefix(sql, "UPDATE ") || strings.HasPrefix(sql, "DELETE ") {
		return 9
	}
	return 0
}

func statementWithoutLeadingComments(statement string) string {
	var lines []string
	for _, line := range strings.Split(statement, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.ToUpper(strings.Join(lines, " "))
}

func splitSQLStatements(sqlText string) []string {
	var parts []string
	start := 0
	inSingle := false
	dollar := ""
	for i := 0; i < len(sqlText); i++ {
		if dollar != "" {
			if strings.HasPrefix(sqlText[i:], dollar) {
				i += len(dollar) - 1
				dollar = ""
			}
			continue
		}
		if inSingle {
			if sqlText[i] == '\'' {
				inSingle = false
			}
			continue
		}
		if sqlText[i] == '\'' {
			inSingle = true
			continue
		}
		if sqlText[i] == '$' {
			if end := strings.IndexByte(sqlText[i+1:], '$'); end >= 0 {
				dollar = sqlText[i : i+end+2]
				i += len(dollar) - 1
			}
			continue
		}
		if sqlText[i] == ';' {
			if part := strings.TrimSpace(sqlText[start:i]); part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	if part := strings.TrimSpace(sqlText[start:]); part != "" {
		parts = append(parts, part)
	}
	return parts
}

func extensionStatementsOnly(sqlText string) string {
	parts := splitSQLStatements(sqlText)
	out := parts[:0]
	for _, part := range parts {
		if strings.HasPrefix(statementWithoutLeadingComments(part), "CREATE EXTENSION ") {
			out = append(out, part)
		}
	}
	return strings.Join(out, ";\n")
}

func latestInitialMigrationIndex(files []string) (int, error) {
	latest := 0
	count := 0
	for i, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return 0, fmt.Errorf("read migration %q: %w", file, err)
		}
		if looksLikeInitialMigrationSQL(string(content)) {
			latest = i
			count++
		}
	}
	if count < 2 {
		return 0, nil
	}
	return latest, nil
}

func looksLikeInitialMigrationSQL(sqlText string) bool {
	creates := 0
	for _, statement := range strings.Split(sqlText, ";") {
		var lines []string
		for _, line := range strings.Split(statement, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			lines = append(lines, line)
		}
		sql := strings.ToUpper(strings.Join(lines, " "))
		if strings.HasPrefix(sql, "CREATE TABLE ") || strings.HasPrefix(sql, "CREATE TYPE ") || strings.HasPrefix(sql, "CREATE EXTENSION ") {
			creates++
		}
		if creates >= 3 {
			return true
		}
	}
	return false
}

func hasUserObjects(ctx context.Context, db Queryer) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT
	(SELECT count(*) FROM pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname <> 'information_schema' AND nspname <> 'public') +
	(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema' AND NOT (n.nspname = 'public' AND c.relname IN ('schema_migrations', 'atlas_schema_revisions'))) +
	(SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace LEFT JOIN pg_class c ON c.oid = t.typrelid WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname <> 'information_schema' AND (t.typtype IN ('e', 'd', 'r') OR (t.typtype = 'c' AND c.relkind = 'c')))`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("inspect dev database objects: %w", err)
	}
	return count > 0, nil
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
