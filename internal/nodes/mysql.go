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
	credential := credentialByType(in.Credentials, "mySql", "mysql", "mysqlDb", "mySqlDb", "credentials")
	dsn := firstNonEmptyNode(stringParam(in.Node.Parameters, "connectionString", "dsn"), credentialString(credential, "connectionString", "dsn"))
	if dsn == "" {
		dsn = mysqlDSN(in.Node.Parameters, credential)
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

func mysqlDSN(params map[string]any, credential map[string]any) string {
	host := firstNonEmptyNode(stringParam(params, "host"), credentialString(credential, "host"))
	if host == "" {
		host = "localhost"
	}
	port := intParam(params, "port", intParam(credential, "port", 3306))
	charset := firstNonEmptyNode(stringParam(params, "charset"), credentialString(credential, "charset"))
	if charset == "" {
		charset = "utf8mb4"
	}
	locationName := firstNonEmptyNode(stringParam(params, "timezone", "location"), credentialString(credential, "timezone", "location"))
	if locationName == "" {
		locationName = "UTC"
	}
	location, err := time.LoadLocation(locationName)
	if err != nil {
		location = time.UTC
	}
	cfg := drivermysql.NewConfig()
	cfg.User = firstNonEmptyNode(stringParam(params, "user", "username"), credentialString(credential, "user", "username"))
	cfg.Passwd = firstNonEmptyNode(stringParam(params, "password"), credentialString(credential, "password"))
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = firstNonEmptyNode(stringParam(params, "database", "db"), credentialString(credential, "database", "db"))
	cfg.ParseTime = true
	cfg.Loc = location
	cfg.Params = map[string]string{"charset": charset}
	if collation := firstNonEmptyNode(stringParam(params, "collation"), credentialString(credential, "collation")); collation != "" {
		cfg.Collation = collation
	}
	timeoutMS := intParam(params, "connectTimeout", intParam(params, "connectionTimeout", intParam(credential, "connectTimeout", intParam(credential, "connectionTimeout", 10000))))
	if timeoutMS <= 0 {
		timeoutMS = 10000
	}
	cfg.Timeout = time.Duration(timeoutMS) * time.Millisecond
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.AllowCleartextPasswords = boolParam(params, "allowCleartextPasswords", boolParam(credential, "allowCleartextPasswords", false))
	ssl := firstNonEmptyNode(stringParam(params, "ssl"), credentialString(credential, "ssl"))
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
