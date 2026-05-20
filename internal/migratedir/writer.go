package migratedir

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/edlundin/edvat/internal/baseatlas"

	"ariga.io/atlas/sql/migrate"
)

type Clock func() time.Time

type Writer struct {
	Dir   string
	Clock Clock
}

func (w Writer) Write(name string, statements []baseatlas.Statement) (string, error) {
	if w.Dir == "" {
		return "", fmt.Errorf("migration dir is required")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("migration name is required")
	}
	if err := os.MkdirAll(w.Dir, 0o755); err != nil {
		return "", fmt.Errorf("create migration dir %q: %w", w.Dir, err)
	}
	clock := w.Clock
	if clock == nil {
		clock = time.Now
	}
	base := filepath.Join(w.Dir, clock().UTC().Format("20060102150405")+"_"+slug(name))
	path := base + ".up.sql"
	downPath := base + ".down.sql"
	for _, candidate := range []string{path, downPath} {
		if _, err := os.Stat(candidate); err == nil {
			return "", fmt.Errorf("migration file already exists: %s", candidate)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat migration file %q: %w", candidate, err)
		}
	}
	if err := os.WriteFile(path, []byte(sqlText(statements)), 0o644); err != nil {
		return "", fmt.Errorf("write migration file %q: %w", path, err)
	}
	if err := os.WriteFile(downPath, []byte(reverseSQLText(statements)), 0o644); err != nil {
		return "", fmt.Errorf("write migration file %q: %w", downPath, err)
	}
	if err := Hash(w.Dir); err != nil {
		return "", err
	}
	return path, nil
}

func Hash(dir string) error {
	local, err := migrate.NewLocalDir(dir)
	if err != nil {
		return fmt.Errorf("open migration dir %q: %w", dir, err)
	}
	sum, err := local.Checksum()
	if err != nil {
		return fmt.Errorf("checksum migration dir %q: %w", dir, err)
	}
	if err := migrate.WriteSumFile(local, sum); err != nil {
		return fmt.Errorf("write atlas.sum in %q: %w", dir, err)
	}
	return nil
}

func reverseSQLText(statements []baseatlas.Statement) string {
	reversed := make([]baseatlas.Statement, 0, len(statements))
	for i := len(statements) - 1; i >= 0; i-- {
		if strings.TrimSpace(statements[i].Reverse) == "" {
			continue
		}
		reversed = append(reversed, baseatlas.Statement{Comment: "reverse " + statements[i].Comment, SQL: statements[i].Reverse})
	}
	return sqlText(reversed)
}

func sqlText(statements []baseatlas.Statement) string {
	var out strings.Builder
	for _, statement := range statements {
		if statement.Comment != "" {
			out.WriteString("-- ")
			out.WriteString(statement.Comment)
			out.WriteString("\n")
		}
		sql := strings.TrimSpace(statement.SQL)
		if sql == "" {
			continue
		}
		out.WriteString(sql)
		if !strings.HasSuffix(sql, ";") {
			out.WriteString(";")
		}
		out.WriteString("\n")
	}
	return out.String()
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func slug(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = slugRe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "migration"
	}
	return name
}
