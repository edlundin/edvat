package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/edlundin/edvat/internal/capabilities"
	"github.com/edlundin/edvat/internal/migratedir"
	"github.com/edlundin/edvat/internal/migrationplan"
	"github.com/edlundin/edvat/internal/project"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "capabilities" {
		fmt.Print(capabilities.Text(capabilities.All()))
		return nil
	}
	if len(args) >= 3 && args[0] == "migrate" && args[1] == "diff" {
		return migrateDiff(args[2:])
	}
	if len(args) >= 2 && args[0] == "migrate" && args[1] == "hash" {
		return migrateHash(args[2:])
	}
	return fmt.Errorf("usage: edvat capabilities\n       edvat migrate diff <name> --env <env> --config atlas.hcl\n       edvat migrate hash --env <env> --config atlas.hcl")
}

func migrateHash(args []string) error {
	fs := flag.NewFlagSet("migrate hash", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	envName := fs.String("env", "local", "project environment")
	configPath := fs.String("config", "atlas.hcl", "project config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := project.LoadEnv(*configPath, *envName)
	if err != nil {
		return err
	}
	if err := migratedir.Hash(cfg.MigrationDir); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "wrote %s\n", cfg.MigrationDir+string(os.PathSeparator)+"atlas.sum")
	return nil
}

func migrateDiff(args []string) error {
	name := args[0]
	fs := flag.NewFlagSet("migrate diff", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	envName := fs.String("env", "local", "project environment")
	configPath := fs.String("config", "atlas.hcl", "project config path")
	devURL := fs.String("dev-url", "", "dev database URL for current-state inspection")
	manageRoles := fs.Bool("manage-roles", false, "enable role DDL generation (destructive/security-sensitive)")
	manageUsers := fs.Bool("manage-users", false, "enable user DDL generation (destructive/security-sensitive; passwords unsupported)")
	allowDestructive := fs.Bool("allow-destructive", false, "allow generated statements marked destructive")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := project.LoadEnv(*configPath, *envName)
	if err != nil {
		return err
	}
	plan, err := migrationplan.Build(context.Background(), cfg, migrationplan.Options{
		Name:             name,
		DevURL:           *devURL,
		ManageRoles:      *manageRoles,
		ManageUsers:      *manageUsers,
		AllowDestructive: *allowDestructive,
	})
	if err != nil {
		return err
	}
	path, err := migratedir.Writer{Dir: cfg.MigrationDir}.Write(name, plan.Statements)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "wrote %s\n", path)
	return nil
}
