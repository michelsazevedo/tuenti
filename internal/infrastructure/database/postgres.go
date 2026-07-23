package database

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
	"github.com/rs/zerolog/log"
)

type PgConn struct {
	db *pgxpool.Pool
}

func NewPgConn(conf *config.Config) (*PgConn, error) {
	pool, err := pgxpool.NewWithConfig(
		context.Background(), pgConfig(conf),
	)
	if err != nil {
		return nil, err
	}
	return &PgConn{db: pool}, nil
}

func pgConfig(conf *config.Config) *pgxpool.Config {
	dbConfig, err := pgxpool.ParseConfig(conf.GetDatabaseURL())

	if err != nil {
		log.Fatal().Msg(fmt.Sprintf("Failed to create a config, error: %s", err.Error()))
	}

	dbConfig.MaxConns = conf.GetPoolSize()
	dbConfig.ConnConfig.ConnectTimeout = conf.Database.Timeout
	dbConfig.ConnConfig.Tracer = otelpgx.NewTracer()
	return dbConfig
}

func (p *PgConn) Ping(ctx context.Context) error {
	return p.db.Ping(ctx)
}

func (p *PgConn) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.db.Begin(ctx)
}

func (p *PgConn) Pool() *pgxpool.Pool {
	return p.db
}

func (p *PgConn) Close() {
	p.db.Close()
}
