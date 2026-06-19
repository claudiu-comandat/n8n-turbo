package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/stdlib"
)

const driverName = "n8nturbopgx"

var registerOnce sync.Once

func Open(dsn string) (*sql.DB, error) {
	registerOnce.Do(func() {
		sql.Register(driverName, rewriteDriver{inner: stdlib.GetDefaultDriver()})
	})
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

type rewriteDriver struct {
	inner driver.Driver
}

func (d rewriteDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return rewriteConn{Conn: conn}, nil
}

type rewriteConn struct {
	driver.Conn
}

func (c rewriteConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(rewriteSQL(query))
}

func (c rewriteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if inner, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return inner.PrepareContext(ctx, rewriteSQL(query))
	}
	return c.Prepare(query)
}

func (c rewriteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if inner, ok := c.Conn.(driver.ExecerContext); ok {
		return inner.ExecContext(ctx, rewriteSQL(query), args)
	}
	return nil, driver.ErrSkip
}

func (c rewriteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if inner, ok := c.Conn.(driver.QueryerContext); ok {
		return inner.QueryContext(ctx, rewriteSQL(query), args)
	}
	return nil, driver.ErrSkip
}

var userTablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+user\b`),
	regexp.MustCompile(`(?i)\bFROM\s+user\b`),
	regexp.MustCompile(`(?i)\bINTO\s+user\b`),
	regexp.MustCompile(`(?i)\bUPDATE\s+user\b`),
}

func rewriteSQL(query string) string {
	query = rewritePlaceholders(query)
	for _, pattern := range userTablePatterns {
		query = pattern.ReplaceAllStringFunc(query, quoteUserTable)
	}
	query = strings.ReplaceAll(query, `strftime('%Y-%m-%d', started_at)`, `to_char(started_at::timestamptz, 'YYYY-MM-DD')`)
	query = strings.ReplaceAll(query, `strftime('%Y-%W', started_at)`, `to_char(started_at::timestamptz, 'IYYY-IW')`)
	query = strings.ReplaceAll(query, `strftime('%Y-%m', started_at)`, `to_char(started_at::timestamptz, 'YYYY-MM')`)
	return query
}

func rewritePlaceholders(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	index := 1
	inSingle := false
	inDouble := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' && !inDouble {
			b.WriteByte(ch)
			if inSingle && i+1 < len(query) && query[i+1] == '\'' {
				i++
				b.WriteByte(query[i])
				continue
			}
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(ch)
			continue
		}
		if ch == '?' && !inSingle && !inDouble {
			b.WriteString(fmt.Sprintf("$%d", index))
			index++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func quoteUserTable(match string) string {
	parts := strings.Fields(match)
	if len(parts) == 0 {
		return match
	}
	parts[len(parts)-1] = `"user"`
	return strings.Join(parts, " ")
}
