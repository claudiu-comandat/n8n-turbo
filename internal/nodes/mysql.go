package nodes

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type MySQL struct{}

func (MySQL) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	dsn := stringParam(in.Node.Parameters, "connectionString", "dsn")
	if dsn == "" {
		dsn = mysqlDSN(in.Node.Parameters)
	}
	db, err := sql.Open("mysql", dsn)
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
	return executeSQLNode(ctx, db, in, mysqlDialect)
}

func mysqlDSN(params map[string]any) string {
	host := stringParam(params, "host")
	if host == "" {
		host = "localhost"
	}
	port := intParam(params, "port", 3306)
	charset := stringParam(params, "charset")
	if charset == "" {
		charset = "utf8mb4"
	}
	locationName := stringParam(params, "timezone", "location")
	if locationName == "" {
		locationName = "UTC"
	}
	location, err := time.LoadLocation(locationName)
	if err != nil {
		location = time.UTC
	}
	cfg := drivermysql.NewConfig()
	cfg.User = stringParam(params, "user", "username")
	cfg.Passwd = stringParam(params, "password")
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = stringParam(params, "database", "db")
	cfg.ParseTime = true
	cfg.Loc = location
	cfg.Params = map[string]string{"charset": charset}
	if collation := stringParam(params, "collation"); collation != "" {
		cfg.Collation = collation
	}
	timeoutMS := intParam(params, "connectTimeout", intParam(params, "connectionTimeout", 10000))
	if timeoutMS <= 0 {
		timeoutMS = 10000
	}
	cfg.Timeout = time.Duration(timeoutMS) * time.Millisecond
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.AllowCleartextPasswords = boolParam(params, "allowCleartextPasswords", false)
	ssl := stringParam(params, "ssl")
	switch ssl {
	case "true", "require":
		cfg.TLSConfig = "true"
	case "skip-verify":
		name := "n8n-turbo-skip-verify"
		_ = drivermysql.RegisterTLSConfig(name, &tls.Config{InsecureSkipVerify: true})
		cfg.TLSConfig = name
	default:
		cfg.TLSConfig = "false"
	}
	return cfg.FormatDSN()
}
