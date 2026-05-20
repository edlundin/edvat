package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/edlundin/edvat/internal/seed"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

var (
	ErrMissingEnv          = errors.New("project: env not found")
	ErrMissingMigrationDir = errors.New("project: env migration.dir is required")
)

type EnvConfig struct {
	Name         string
	SchemaPaths  []string
	MigrationDir string
	SeedMode     seed.Mode
	SeedPaths    []string
}

type hclSchemaSource struct {
	Paths []string
}

func LoadEnv(configPath, envName string) (EnvConfig, error) {
	if envName == "" {
		envName = "local"
	}
	src, err := os.ReadFile(configPath)
	if err != nil {
		return EnvConfig{}, fmt.Errorf("read project config %q: %w", configPath, err)
	}
	file, diags := hclsyntax.ParseConfig(src, configPath, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return EnvConfig{}, fmt.Errorf("parse project config: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return EnvConfig{}, fmt.Errorf("parse project config: unexpected body type %T", file.Body)
	}

	baseDir := filepath.Dir(configPath)
	sources, err := loadHCLSchemaSources(body, baseDir)
	if err != nil {
		return EnvConfig{}, err
	}
	for _, block := range body.Blocks {
		if block.Type != "env" || len(block.Labels) != 1 || block.Labels[0] != envName {
			continue
		}
		cfg, err := loadEnvBlock(block, baseDir, sources)
		if err != nil {
			return EnvConfig{}, err
		}
		cfg.Name = envName
		if cfg.MigrationDir == "" {
			return EnvConfig{}, ErrMissingMigrationDir
		}
		return cfg, nil
	}
	return EnvConfig{}, fmt.Errorf("%w: %s", ErrMissingEnv, envName)
}

func loadHCLSchemaSources(body *hclsyntax.Body, baseDir string) (map[string]hclSchemaSource, error) {
	sources := map[string]hclSchemaSource{}
	for _, block := range body.Blocks {
		if block.Type != "data" || len(block.Labels) != 2 || block.Labels[0] != "hcl_schema" {
			continue
		}
		attrs := block.Body.Attributes
		var paths []string
		if attr, ok := attrs["path"]; ok {
			path, err := stringExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode data.hcl_schema.%s.path: %w", block.Labels[1], err)
			}
			paths = append(paths, normalizePath(baseDir, path))
		}
		if attr, ok := attrs["paths"]; ok {
			values, err := stringListExpr(attr.Expr)
			if err != nil {
				return nil, fmt.Errorf("decode data.hcl_schema.%s.paths: %w", block.Labels[1], err)
			}
			for _, path := range values {
				paths = append(paths, normalizePath(baseDir, path))
			}
		}
		sources[block.Labels[1]] = hclSchemaSource{Paths: paths}
	}
	return sources, nil
}

func loadEnvBlock(block *hclsyntax.Block, baseDir string, sources map[string]hclSchemaSource) (EnvConfig, error) {
	cfg := EnvConfig{SeedMode: seed.ModeInsert}
	if attr, ok := block.Body.Attributes["src"]; ok {
		paths, err := schemaPathsFromExpr(attr.Expr, baseDir, sources)
		if err != nil {
			return EnvConfig{}, fmt.Errorf("decode env.%s.src: %w", block.Labels[0], err)
		}
		cfg.SchemaPaths = append(cfg.SchemaPaths, paths...)
	}
	for _, nested := range block.Body.Blocks {
		switch nested.Type {
		case "schema":
			attr, ok := nested.Body.Attributes["src"]
			if !ok {
				continue
			}
			paths, err := schemaPathsFromExpr(attr.Expr, baseDir, sources)
			if err != nil {
				return EnvConfig{}, fmt.Errorf("decode env.%s.schema.src: %w", block.Labels[0], err)
			}
			cfg.SchemaPaths = append(cfg.SchemaPaths, paths...)
		case "migration":
			attr, ok := nested.Body.Attributes["dir"]
			if !ok {
				continue
			}
			dir, err := stringExpr(attr.Expr)
			if err != nil {
				return EnvConfig{}, fmt.Errorf("decode env.%s.migration.dir: %w", block.Labels[0], err)
			}
			cfg.MigrationDir = normalizePath(baseDir, dir)
		case "data":
			if attr, ok := nested.Body.Attributes["mode"]; ok {
				mode, err := modeExpr(attr.Expr)
				if err != nil {
					return EnvConfig{}, fmt.Errorf("decode env.%s.data.mode: %w", block.Labels[0], err)
				}
				cfg.SeedMode = mode
			}
			if attr, ok := nested.Body.Attributes["src"]; ok {
				paths, err := schemaPathsFromExpr(attr.Expr, baseDir, sources)
				if err != nil {
					return EnvConfig{}, fmt.Errorf("decode env.%s.data.src: %w", block.Labels[0], err)
				}
				cfg.SeedPaths = append(cfg.SeedPaths, paths...)
			}
		}
	}
	return cfg, nil
}

func schemaPathsFromExpr(expr hclsyntax.Expression, baseDir string, sources map[string]hclSchemaSource) ([]string, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 4 && parts[0] == "data" && parts[1] == "hcl_schema" && parts[3] == "url" {
			return expandSchemaPaths(sources[parts[2]].Paths)
		}
	}
	if paths, err := stringListExpr(expr); err == nil {
		out := make([]string, 0, len(paths))
		for _, path := range paths {
			out = append(out, normalizePath(baseDir, path))
		}
		return expandSchemaPaths(out)
	}
	path, err := stringExpr(expr)
	if err != nil {
		return nil, err
	}
	return expandSchemaPaths([]string{normalizePath(baseDir, path)})
}

func expandSchemaPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			out = append(out, path)
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("read schema directory %q: %w", path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".hcl") {
				continue
			}
			out = append(out, filepath.Join(path, entry.Name()))
		}
	}
	return out, nil
}

func stringExpr(expr hclsyntax.Expression) (string, error) {
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return "", fmt.Errorf("%s", diags.Error())
	}
	if value.Type() != cty.String || value.IsNull() {
		return "", fmt.Errorf("expected string, got %s", value.Type().FriendlyName())
	}
	return value.AsString(), nil
}

func stringListExpr(expr hclsyntax.Expression) ([]string, error) {
	value, diags := expr.Value(nil)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s", diags.Error())
	}
	if value.IsNull() || !value.CanIterateElements() {
		return nil, fmt.Errorf("expected list of strings")
	}
	var values []string
	it := value.ElementIterator()
	for it.Next() {
		_, item := it.Element()
		if item.Type() != cty.String || item.IsNull() {
			return nil, fmt.Errorf("expected list of strings")
		}
		values = append(values, item.AsString())
	}
	return values, nil
}

func modeExpr(expr hclsyntax.Expression) (seed.Mode, error) {
	if traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr); ok {
		parts := traversalParts(traversal.Traversal)
		if len(parts) == 1 {
			return validateMode(parts[0])
		}
	}
	value, err := stringExpr(expr)
	if err != nil {
		return "", err
	}
	return validateMode(value)
}

func validateMode(value string) (seed.Mode, error) {
	mode := seed.Mode(strings.ToUpper(value))
	switch mode {
	case seed.ModeInsert, seed.ModeUpsert, seed.ModeSync:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported seed mode %q", value)
	}
}

func normalizePath(baseDir, value string) string {
	path := strings.TrimPrefix(value, "file://")
	if path == "" {
		path = value
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func traversalParts(traversal hcl.Traversal) []string {
	parts := make([]string, 0, len(traversal))
	for _, traverser := range traversal {
		switch t := traverser.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, t.Name)
		case hcl.TraverseAttr:
			parts = append(parts, t.Name)
		}
	}
	return parts
}
