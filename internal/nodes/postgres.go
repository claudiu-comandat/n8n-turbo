package nodes

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Postgres struct{}

func (Postgres) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	dsn := stringParam(in.Node.Parameters, "connectionString", "dsn")
	if dsn == "" {
		dsn = postgresDSN(in.Node.Parameters)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	maxConnections := intParam(in.Node.Parameters, "maxConnections", 5)
	if maxConnections < 1 {
		maxConnections = 1
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return executeSQLNode(ctx, db, in, postgresDialect)
}

func postgresDSN(params map[string]any) string {
	host := stringParam(params, "host")
	if host == "" {
		host = "localhost"
	}
	port := intParam(params, "port", 5432)
	database := stringParam(params, "database", "db")
	user := stringParam(params, "user", "username")
	password := stringParam(params, "password")
	sslmode := stringParam(params, "ssl", "sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + database,
	}
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	query := u.Query()
	query.Set("sslmode", sslmode)
	if timeout := intParam(params, "connectionTimeout", 0); timeout > 0 {
		query.Set("connect_timeout", strconv.Itoa(timeout))
	}
	u.RawQuery = query.Encode()
	return u.String()
}
