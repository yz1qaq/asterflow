package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("asterflow migrate: %v", err)
		os.Exit(1)
	}
}

func run(args []string) (runErr error) {
	flags := flag.NewFlagSet(
		"asterflow-migrate",
		flag.ContinueOnError,
	)

	up := flags.Bool("up", false, "apply all pending migrations")
	down := flags.Int("down", 0, "roll back N migrations")
	showVersion := flags.Bool(
		"version", false, "show current migration version",
	)
	path := flags.String("path", "migrations", "migration directory")
	databaseEnv := flags.String(
		"database-env",
		"ASTERFLOW_DATABASE_URL",
		"environment variable containing the database URL",
	)

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}

	downSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "down" {
			downSet = true
		}
	})
	if downSet && *down <= 0 {
		return fmt.Errorf("-down must be greater than zero")
	}

	actions := 0
	for _, enabled := range []bool{*up, downSet, *showVersion} {
		if enabled {
			actions++
		}
	}
	if actions != 1 {
		return fmt.Errorf(
			"choose exactly one action: -up, -down N, or -version",
		)
	}
	if strings.TrimSpace(*path) == "" {
		return fmt.Errorf("migration path is required")
	}
	if strings.TrimSpace(*databaseEnv) == "" {
		return fmt.Errorf("database environment variable name is required")
	}

	rawURL := os.Getenv(*databaseEnv)
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("%s is required", *databaseEnv)
	}

	databaseURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid database URL")
	}
	switch strings.ToLower(databaseURL.Scheme) {
	case "postgres", "postgresql", "pgx5":
		databaseURL.Scheme = "pgx5"
	default:
		return fmt.Errorf(
			"database URL must use postgres, postgresql, or pgx5",
		)
	}

	query, err := url.ParseQuery(databaseURL.RawQuery)
	if err != nil {
		return fmt.Errorf("invalid database URL query")
	}

	if query.Get("connect_timeout") == "" {
		query.Set("connect_timeout", "5")
	}
	databaseURL.RawQuery = query.Encode()

	absolutePath, err := filepath.Abs(*path)
	if err != nil {
		return fmt.Errorf("resolve migration path: %w", err)
	}
	sourceURL := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(absolutePath),
	}

	m, err := migrate.New(sourceURL.String(), databaseURL.String())
	if err != nil {
		return fmt.Errorf(
			"initialize migrations: check database access and migration files",
		)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		runErr = errors.Join(runErr, sourceErr, databaseErr)
	}()

	switch {
	case *showVersion:
		version, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("version=none")
			return nil
		}
		if err != nil {
			return fmt.Errorf("read migration version: %w", err)
		}
		fmt.Printf("version=%d dirty=%t\n", version, dirty)

	case *up:
		err := m.Up()
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("migrations are up to date")
			return nil
		}
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		fmt.Println("migrations applied")

	default:
		err := m.Steps(-*down)
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("no migrations to roll back")
			return nil
		}
		if err != nil {
			return fmt.Errorf("roll back migrations: %w", err)
		}
		fmt.Printf("rolled back %d migration(s)\n", *down)
	}

	return nil
}
