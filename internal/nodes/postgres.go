package nodes

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type Postgres struct{}

func (Postgres) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	credential := credentialByType(in.Credentials, "postgres", "postgresdb", "postgresql", "credentials")
	dsn := PostgresDSN(in.Node.Parameters, credential)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	maxConnections := intParam(in.Node.Parameters, "maxConnections", intParam(credential, "maxConnections", 5))
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

func PostgresTestConnection(ctx context.Context, data map[string]any) error {
	db, err := sql.Open("pgx", PostgresDSN(nil, data))
	if err != nil {
		return err
	}
	defer db.Close()
	maxConnections := intParam(data, "maxConnections", 1)
	if maxConnections < 1 {
		maxConnections = 1
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	timeout := time.Duration(intParam(data, "connectionTimeout", 10)) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return db.PingContext(pingCtx)
}

func PostgresDSN(params map[string]any, credential map[string]any) string {
	dsn := firstNonEmptyNode(stringParam(params, "connectionString", "dsn"), credentialString(credential, "connectionString", "dsn"))
	if dsn != "" {
		return dsn
	}
	host := firstNonEmptyNode(stringParam(params, "host"), credentialString(credential, "host"), "localhost")
	port := intParam(params, "port", intParam(credential, "port", 5432))
	database := firstNonEmptyNode(stringParam(params, "database", "db"), credentialString(credential, "database", "db"), "db")
	user := firstNonEmptyNode(stringParam(params, "user", "username"), credentialString(credential, "user", "username"))
	password := firstNonEmptyNode(stringParam(params, "password"), credentialString(credential, "password"))
	sslmode := normalizePostgresSSLMode(firstNonEmptyNode(stringParam(params, "ssl", "sslmode"), credentialString(credential, "ssl", "sslmode")))
	if boolParam(params, "ignoreSSLIssues", boolParam(credential, "ignoreSSLIssues", false)) && (sslmode == "verify-ca" || sslmode == "verify-full") {
		sslmode = "require"
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
	} else if timeout := intParam(credential, "connectionTimeout", 0); timeout > 0 {
		query.Set("connect_timeout", strconv.Itoa(timeout))
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func normalizePostgresSSLMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "false", "disabled", "disable":
		return "disable"
	case "true", "enabled", "enable", "require":
		return "require"
	case "allow", "prefer", "verify-ca", "verify-full":
		return normalized
	default:
		return normalized
	}
}
