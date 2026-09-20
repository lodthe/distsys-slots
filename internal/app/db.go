package app

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func OpenDB(ctx context.Context, connection string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	cfg.MaxConns = 20
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(781198234)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, f := range files {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, f.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, e := migrations.ReadFile("migrations/" + f.Name())
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", f.Name(), err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, f.Name()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func objects(ctx context.Context, q querier, sql string, args ...any) ([]map[string]any, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToMap)
	if out == nil {
		out = []map[string]any{}
	}
	return out, err
}

type problem struct {
	Status        int
	Code, Message string
}

func (p *problem) Error() string    { return p.Message }
func bad(message string) error      { return &problem{400, "invalid_input", message} }
func conflict(message string) error { return &problem{409, "conflict", message} }
func missing() error                { return &problem{404, "not_found", "Объект не найден"} }

func transaction(ctx context.Context, db *pgxpool.Pool, fn func(pgx.Tx) error) error {
	for i := 0; i < 3; i++ {
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		err = fn(tx)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		var pe *pgconn.PgError
		if errors.As(err, &pe) && (pe.Code == "40P01" || pe.Code == "40001") {
			continue
		}
		if errors.As(err, &pe) && pe.Code == "23505" {
			return conflict("Слот уже занят или данные уже существуют")
		}
		return err
	}
	return conflict("Расписание изменяется, повторите запрос")
}
