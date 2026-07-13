package nodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	_ "modernc.org/sqlite"
)

// Unique in-memory DB name per execution (shared cache would leak data across executions).
var sqliteMemSeq atomic.Uint64

type SQLite struct{}

func (SQLite) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	path := stringParam(in.Node.Parameters, "database", "databasePath", "file")
	if path == "" {
		path = ":memory:"
	}
	db, err := sql.Open("sqlite", sqliteDSN(path, boolParam(in.Node.Parameters, "readOnly", false)))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return nil, err
	}
	if timeout := intParam(in.Node.Parameters, "busyTimeout", 5000); timeout > 0 {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", timeout)); err != nil {
			return nil, err
		}
	}
	if boolParam(in.Node.Parameters, "walMode", false) && path != ":memory:" {
		if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			return nil, err
		}
	}
	switch strings.ToLower(stringParam(in.Node.Parameters, "operation")) {
	case "", "executequery":
		return sqliteExecuteQuery(ctx, db, in)
	case "insert":
		return sqliteInsert(ctx, db, in)
	case "update":
		return sqliteUpdate(ctx, db, in)
	case "delete":
		return sqliteDelete(ctx, db, in)
	case "select":
		return sqliteSelect(ctx, db, in)
	default:
		return nil, fmt.Errorf("unsupported sqlite operation %q", stringParam(in.Node.Parameters, "operation"))
	}
}

func sqliteDSN(path string, readOnly bool) string {
	if path == ":memory:" {
		return fmt.Sprintf("file:n8n-mem-%d?mode=memory&cache=shared", sqliteMemSeq.Add(1))
	}
	if strings.HasPrefix(path, "file:") {
		return path
	}
	mode := "rwc"
	if readOnly {
		mode = "ro"
	}
	return fmt.Sprintf("file:%s?mode=%s&_foreign_keys=on", path, mode)
}

func sqliteExecuteQuery(ctx context.Context, db *sql.DB, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	queryBatching := strings.ToLower(firstNonEmptyNode(stringParam(sqlOptions(in.Node.Parameters), "queryBatching"), stringParam(in.Node.Parameters, "queryBatching"), "independently"))
	if queryBatching == "singlequery" || queryBatching == "single" {
		items = items[:1]
	}
	if queryBatching == "transaction" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		output, err := sqliteExecuteQueryWithRunner(ctx, tx, in, items)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return output, nil
	}
	return sqliteExecuteQueryWithRunner(ctx, db, in, items)
}

type sqliteRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func sqliteExecuteQueryWithRunner(ctx context.Context, runner sqliteRunner, in engine.ExecuteInput, items []dataplane.Item) (dataplane.Output, error) {
	output := make([]dataplane.Item, 0)
	for index := range items {
		query := strings.TrimSpace(fmt.Sprint(resolveValue(in, items, index, in.Node.Parameters["query"])))
		if query == "" {
			return nil, fmt.Errorf("sqlite query is required")
		}
		args := sqliteArgs(in, items, index)
		if sqliteReturnsRows(query) {
			rows, err := sqliteQueryRowsWithRunner(ctx, runner, query, args...)
			if err != nil {
				return nil, err
			}
			output = append(output, rows...)
			continue
		}
		if _, err := runner.ExecContext(ctx, query, args...); err != nil {
			return nil, err
		}
		output = append(output, dataplane.Item{JSON: map[string]any{"success": true}})
	}
	return dataplane.MainOutput(output), nil
}

func sqliteInsert(ctx context.Context, db *sql.DB, in engine.ExecuteInput) (dataplane.Output, error) {
	table := stringParam(in.Node.Parameters, "table")
	if table == "" {
		return nil, fmt.Errorf("sqlite table is required")
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		columns := sqliteColumns(in.Node.Parameters, item.JSON)
		if len(columns) == 0 {
			return nil, fmt.Errorf("sqlite insert requires columns")
		}
		args := make([]any, 0, len(columns))
		placeholders := make([]string, 0, len(columns))
		for _, column := range columns {
			args = append(args, item.JSON[column])
			placeholders = append(placeholders, "?")
		}
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", sqliteIdent(table), sqliteIdentList(columns), strings.Join(placeholders, ", "))
		if returning := sqliteReturningClause(in.Node.Parameters); returning != "" {
			rows, err := sqliteQueryRows(ctx, db, query+" RETURNING "+returning, args...)
			if err != nil {
				return nil, err
			}
			output = append(output, rows...)
			continue
		}
		result, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		output = append(output, sqliteResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqliteUpdate(ctx context.Context, db *sql.DB, in engine.ExecuteInput) (dataplane.Output, error) {
	table := stringParam(in.Node.Parameters, "table")
	key := stringParam(in.Node.Parameters, "updateKey", "whereKey")
	if table == "" || key == "" {
		return nil, fmt.Errorf("sqlite update requires table and updateKey")
	}
	items := firstInput(in.InputData)
	output := make([]dataplane.Item, 0, len(items))
	for _, item := range items {
		columns := sqliteColumns(in.Node.Parameters, item.JSON)
		filtered := make([]string, 0, len(columns))
		for _, column := range columns {
			if column != key {
				filtered = append(filtered, column)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("sqlite update requires non-key columns")
		}
		set := make([]string, 0, len(filtered))
		args := make([]any, 0, len(filtered)+1)
		for _, column := range filtered {
			set = append(set, sqliteIdent(column)+" = ?")
			args = append(args, item.JSON[column])
		}
		args = append(args, item.JSON[key])
		query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?", sqliteIdent(table), strings.Join(set, ", "), sqliteIdent(key))
		if returning := sqliteReturningClause(in.Node.Parameters); returning != "" {
			rows, err := sqliteQueryRows(ctx, db, query+" RETURNING "+returning, args...)
			if err != nil {
				return nil, err
			}
			output = append(output, rows...)
			continue
		}
		result, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		output = append(output, sqliteResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqliteDelete(ctx context.Context, db *sql.DB, in engine.ExecuteInput) (dataplane.Output, error) {
	table := stringParam(in.Node.Parameters, "table")
	key := stringParam(in.Node.Parameters, "deleteKey", "whereKey")
	value := in.Node.Parameters["deleteValue"]
	if table == "" || key == "" {
		return nil, fmt.Errorf("sqlite delete requires table and deleteKey")
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{key: value}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		whereValue := item.JSON[key]
		if value != nil {
			whereValue = resolveValue(in, items, index, value)
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", sqliteIdent(table), sqliteIdent(key))
		if returning := sqliteReturningClause(in.Node.Parameters); returning != "" {
			rows, err := sqliteQueryRows(ctx, db, query+" RETURNING "+returning, whereValue)
			if err != nil {
				return nil, err
			}
			output = append(output, rows...)
			continue
		}
		result, err := db.ExecContext(ctx, query, whereValue)
		if err != nil {
			return nil, err
		}
		output = append(output, sqliteResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqliteSelect(ctx context.Context, db *sql.DB, in engine.ExecuteInput) (dataplane.Output, error) {
	table := stringParam(in.Node.Parameters, "table")
	if table == "" {
		return nil, fmt.Errorf("sqlite select requires table")
	}
	columns := strings.TrimSpace(stringParam(in.Node.Parameters, "columns", "returnFields"))
	if columns == "" {
		columns = "*"
	} else {
		columns = sqliteIdentList(splitCSV(columns))
	}
	query := fmt.Sprintf("SELECT %s FROM %s", columns, sqliteIdent(table))
	if where := strings.TrimSpace(stringParam(in.Node.Parameters, "where")); where != "" {
		query += " WHERE " + where
	}
	limit := intParam(in.Node.Parameters, "limit", 0)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset := intParam(in.Node.Parameters, "offset", 0); offset > 0 {
		if limit <= 0 {
			query += " LIMIT -1"
		}
		query += fmt.Sprintf(" OFFSET %d", offset)
	}
	returnRows, err := sqliteQueryRows(ctx, db, query, sqliteArgs(in, firstInput(in.InputData), 0)...)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(returnRows), nil
}

func sqliteQueryRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]dataplane.Item, error) {
	return sqliteQueryRowsWithRunner(ctx, db, query, args...)
}

func sqliteQueryRowsWithRunner(ctx context.Context, runner sqliteRunner, query string, args ...any) ([]dataplane.Item, error) {
	rows, err := runner.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	output := make([]dataplane.Item, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		item := dataplane.Item{JSON: map[string]any{}}
		for i, column := range columns {
			item.JSON[column] = sqliteValue(values[i])
		}
		output = append(output, item)
	}
	return output, rows.Err()
}

func sqliteResultItem(result sql.Result) dataplane.Item {
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	return dataplane.Item{JSON: map[string]any{"rowsAffected": rowsAffected, "lastInsertId": lastInsertID}}
}

func sqliteArgs(in engine.ExecuteInput, items []dataplane.Item, index int) []any {
	raw, ok := in.Node.Parameters["queryParams"]
	if !ok {
		raw = in.Node.Parameters["parameters"]
	}
	if object, ok := raw.(map[string]any); ok {
		if values, ok := object["values"].([]any); ok {
			raw = values
		} else if values, ok := object["queryParams"].([]any); ok {
			raw = values
		}
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	args := make([]any, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			value = firstNonNil(object["value"], object["name"])
		}
		args = append(args, resolveValue(in, items, index, value))
	}
	return args
}

func sqliteColumns(params map[string]any, item map[string]any) []string {
	if raw := stringParam(params, "columns"); raw != "" {
		return splitCSV(raw)
	}
	columns := make([]string, 0, len(item))
	for key := range item {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	return columns
}

func sqliteReturnsRows(query string) bool {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return false
	}
	if strings.Contains(strings.ToLower(query), " returning ") {
		return true
	}
	first := strings.ToLower(fields[0])
	switch first {
	case "select", "pragma", "with", "explain":
		return true
	default:
		return false
	}
}

func sqliteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sqliteIdentList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, sqliteIdent(value))
	}
	return strings.Join(quoted, ", ")
}

func splitCSV(value string) []string {
	result := []string{}
	start := 0
	depth := 0
	quote := rune(0)
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 {
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '"', '\'':
			quote = char
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if part := strings.TrimSpace(value[start:index]); part != "" {
					result = append(result, part)
				}
				start = index + len(string(char))
			}
		}
	}
	if part := strings.TrimSpace(value[start:]); part != "" {
		result = append(result, part)
	}
	return result
}

func sqliteValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		var decoded any
		if json.Unmarshal(bytes, &decoded) == nil {
			return decoded
		}
		return string(bytes)
	}
	if text, ok := value.(string); ok && (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			return decoded
		}
	}
	return value
}

func sqliteReturningClause(params map[string]any) string {
	options := sqlOptions(params)
	raw := firstNonEmptyNode(stringParam(params, "returnFields"), stringParam(options, "outputColumns"), stringParam(options, "returnFields"))
	if raw == "" && boolParam(options, "returnId", false) {
		raw = "rowid"
	}
	if raw == "" {
		return ""
	}
	if raw == "*" {
		return "*"
	}
	return sqliteIdentList(splitCSV(raw))
}
