package postgres

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func RunMigrations(ctx context.Context, databaseURL, migrationsDir string) error {
	if migrationsDir == "" {
		return fmt.Errorf("migrations dir is required")
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	db := stdlib.OpenDB(*cfg)
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	goose.SetLogger(log.New(jsonWriter{}, "", 0))

	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}

	return nil
}

type jsonWriter struct{}

func (jsonWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	line := fmt.Sprintf(`{"level":"INFO","msg":%q}`+"\n", msg)
	_, _ = os.Stdout.Write([]byte(line))
	return len(p), nil
}
