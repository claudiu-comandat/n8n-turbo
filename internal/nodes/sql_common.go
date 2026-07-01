package nodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
)

type sqlDialect struct {
	Name        string
	Quote       string
	Placeholder func(int) string
}

var postgresDialect = sqlDialect{Name: "postgres", Quote: `"`, Placeholder: func(index int) string { return fmt.Sprintf("$%d", index) }}
var mysqlDialect = sqlDialect{Name: "mysql", Quote: "`", Placeholder: func(index int) string { return "?" }}

func executeSQLNode(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	operation := sqlOperation(in.Node.Parameters)
	switch operation {
	case "executequery":
		return sqlExecuteQuery(ctx, db, in, dialect)
	case "insert":
		return sqlInsert(ctx, db, in, dialect)
	case "update":
		return sqlUpdate(ctx, db, in, dialect)
	case "upsert", "insertorupdate":
		return sqlUpsert(ctx, db, in, dialect)
	case "delete", "deletetable":
		return sqlDelete(ctx, db, in, dialect)
	case "select":
		return sqlSelect(ctx, db, in, dialect)
	default:
		return nil, fmt.Errorf("unsupported %s operation %q", dialect.Name, stringParam(in.Node.Parameters, "operation"))
	}
}

func sqlOperation(params map[string]any) string {
	operation := strings.ToLower(stringParam(params, "operation"))
	if operation != "" {
		return operation
	}
	if strings.TrimSpace(stringParam(params, "query")) != "" {
		return "executequery"
	}
	return "insert"
}

func sqlExecuteQuery(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
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
		output, err := sqlExecuteQueryWithRunner(ctx, tx, in, dialect, items)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return output, nil
	}
	return sqlExecuteQueryWithRunner(ctx, db, in, dialect, items)
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func sqlExecuteQueryWithRunner(ctx context.Context, runner sqlRunner, in engine.ExecuteInput, dialect sqlDialect, items []dataplane.Item) (dataplane.Output, error) {
	output := make([]dataplane.Item, 0)
	for index := range items {
		query := strings.TrimSpace(fmt.Sprint(resolveValue(in, items, index, in.Node.Parameters["query"])))
		if query == "" {
			return nil, fmt.Errorf("%s query is required", dialect.Name)
		}
		args := sqlArgsForQuery(in, items, index, query, dialect)
		if statements := sqlStatements(query); len(statements) > 1 {
			argOffset := 0
			for _, statement := range statements {
				statementArgs, usedArgs := sqlStatementArgs(statement, dialect, args, argOffset)
				argOffset += usedArgs
				if sqlReturnsRows(statement) {
					rows, err := sqlQueryRowsWithRunner(ctx, runner, statement, statementArgs...)
					if err != nil {
						return nil, err
					}
					output = append(output, rows...)
					continue
				}
				result, err := runner.ExecContext(ctx, statement, statementArgs...)
				if err != nil {
					return nil, err
				}
				output = append(output, sqlResultItem(result))
			}
			continue
		}
		if sqlReturnsRows(query) {
			rows, err := sqlQueryRowsWithRunner(ctx, runner, query, args...)
			if err != nil {
				return nil, err
			}
			output = append(output, rows...)
			continue
		}
		result, err := runner.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		output = append(output, sqlResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqlLegacyExecuteQuery(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	query := strings.TrimSpace(fmt.Sprint(resolveValue(in, items, 0, in.Node.Parameters["query"])))
	if query == "" {
		return nil, fmt.Errorf("%s query is required", dialect.Name)
	}
	args := sqlArgsForQuery(in, items, 0, query, dialect)
	if sqlReturnsRows(query) {
		rows, err := sqlQueryRows(ctx, db, query, args...)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput(rows), nil
	}
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput([]dataplane.Item{sqlResultItem(result)}), nil
}

func sqlInsert(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	table := sqlTableName(in.Node.Parameters, dialect)
	if table == "" {
		return nil, fmt.Errorf("%s table is required", dialect.Name)
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		sourceItem := sqlMappedItem(in, items, index, item.JSON, in.Node.Parameters)
		columns := sqlColumns(in.Node.Parameters, sourceItem)
		if len(columns) == 0 {
			return nil, fmt.Errorf("%s insert requires columns", dialect.Name)
		}
		args := make([]any, 0, len(columns))
		placeholders := make([]string, 0, len(columns))
		for index, column := range columns {
			args = append(args, sourceItem[column])
			placeholders = append(placeholders, dialect.Placeholder(index+1))
		}
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, sqlIdentList(columns, dialect), strings.Join(placeholders, ", "))
		if boolParam(sqlOptions(in.Node.Parameters), "skipOnConflict", false) && dialect.Name == "postgres" {
			query += " ON CONFLICT DO NOTHING"
		}
		if returning := sqlReturningClause(in.Node.Parameters, dialect); returning != "" {
			rows, err := sqlQueryRows(ctx, db, query+" RETURNING "+returning, args...)
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
		output = append(output, sqlResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqlUpsert(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	table := sqlTableName(in.Node.Parameters, dialect)
	if table == "" {
		return nil, fmt.Errorf("%s table is required", dialect.Name)
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		sourceItem := sqlMappedItem(in, items, index, item.JSON, in.Node.Parameters)
		columns := sqlColumns(in.Node.Parameters, sourceItem)
		if len(columns) == 0 {
			return nil, fmt.Errorf("%s upsert requires columns", dialect.Name)
		}
		matchColumns := sqlMatchingColumns(in.Node.Parameters, columns)
		if len(matchColumns) == 0 {
			matchColumns = columns[:1]
		}
		query, updateColumns, err := sqlUpsertQuery(table, columns, matchColumns, dialect)
		if err != nil {
			return nil, err
		}
		args := make([]any, 0, len(columns)+len(updateColumns))
		for _, column := range columns {
			args = append(args, sourceItem[column])
		}
		for _, column := range updateColumns {
			args = append(args, sourceItem[column])
		}
		if returning := sqlReturningClause(in.Node.Parameters, dialect); returning != "" {
			rows, err := sqlQueryRows(ctx, db, query+" RETURNING "+returning, args...)
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
		output = append(output, sqlResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqlUpsertQuery(table string, columns []string, matchColumns []string, dialect sqlDialect) (string, []string, error) {
	placeholders := make([]string, 0, len(columns))
	for index := range columns {
		placeholders = append(placeholders, dialect.Placeholder(index+1))
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, sqlIdentList(columns, dialect), strings.Join(placeholders, ", "))

	updateColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		if !containsString(matchColumns, column) {
			updateColumns = append(updateColumns, column)
		}
	}

	if dialect.Name == "mysql" {
		if len(updateColumns) == 0 {
			return "", nil, fmt.Errorf("%s upsert requires non-key columns", dialect.Name)
		}
		setParts := make([]string, 0, len(updateColumns))
		for index, column := range updateColumns {
			setParts = append(setParts, fmt.Sprintf("%s = %s", sqlIdent(column, dialect), dialect.Placeholder(len(columns)+index+1)))
		}
		return query + " ON DUPLICATE KEY UPDATE " + strings.Join(setParts, ", "), updateColumns, nil
	}

	query += " ON CONFLICT (" + sqlIdentList(matchColumns, dialect) + ")"
	if len(updateColumns) == 0 {
		return query + " DO NOTHING", nil, nil
	}
	setParts := make([]string, 0, len(updateColumns))
	for _, column := range updateColumns {
		setParts = append(setParts, fmt.Sprintf("%s = EXCLUDED.%s", sqlIdent(column, dialect), sqlIdent(column, dialect)))
	}
	return query + " DO UPDATE SET " + strings.Join(setParts, ", "), nil, nil
}

func sqlUpdate(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	table := sqlTableName(in.Node.Parameters, dialect)
	key := stringParam(in.Node.Parameters, "updateKey", "whereKey")
	if table == "" || key == "" {
		return nil, fmt.Errorf("%s update requires table and updateKey", dialect.Name)
	}
	items := firstInput(in.InputData)
	output := make([]dataplane.Item, 0, len(items))
	for index, item := range items {
		sourceItem := sqlMappedItem(in, items, index, item.JSON, in.Node.Parameters)
		columns := sqlColumns(in.Node.Parameters, sourceItem)
		rowKey := key
		matchColumns := sqlMatchingColumns(in.Node.Parameters, []string{rowKey})
		if len(matchColumns) > 0 {
			rowKey = matchColumns[0]
		}
		filtered := make([]string, 0, len(columns))
		for _, column := range columns {
			if column != rowKey {
				filtered = append(filtered, column)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("%s update requires non-key columns", dialect.Name)
		}
		set := make([]string, 0, len(filtered))
		args := make([]any, 0, len(filtered)+1)
		for index, column := range filtered {
			set = append(set, sqlIdent(column, dialect)+" = "+dialect.Placeholder(index+1))
			args = append(args, sourceItem[column])
		}
		args = append(args, sourceItem[rowKey])
		query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = %s", table, strings.Join(set, ", "), sqlIdent(rowKey, dialect), dialect.Placeholder(len(args)))
		if returning := sqlReturningClause(in.Node.Parameters, dialect); returning != "" {
			rows, err := sqlQueryRows(ctx, db, query+" RETURNING "+returning, args...)
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
		output = append(output, sqlResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqlDelete(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	table := sqlTableName(in.Node.Parameters, dialect)
	if table == "" {
		return nil, fmt.Errorf("%s delete requires table", dialect.Name)
	}
	options := sqlOptions(in.Node.Parameters)
	deleteCommand := strings.ToLower(stringParam(in.Node.Parameters, "deleteCommand"))
	if deleteCommand == "" && (stringParam(in.Node.Parameters, "deleteKey", "whereKey") != "" || in.Node.Parameters["deleteValue"] != nil) {
		deleteCommand = "__legacy__"
	}
	switch deleteCommand {
	case "", "truncate":
		query := fmt.Sprintf("TRUNCATE TABLE %s", table)
		if boolParam(options, "restartSequences", false) {
			query += " RESTART IDENTITY"
		}
		if boolParam(options, "cascade", false) {
			query += " CASCADE"
		}
		result, err := db.ExecContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput([]dataplane.Item{sqlResultItem(result)}), nil
	case "drop":
		query := fmt.Sprintf("DROP TABLE IF EXISTS %s", table)
		if boolParam(options, "cascade", false) {
			query += " CASCADE"
		}
		result, err := db.ExecContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput([]dataplane.Item{sqlResultItem(result)}), nil
	case "__legacy__":
		key := stringParam(in.Node.Parameters, "deleteKey", "whereKey")
		value := in.Node.Parameters["deleteValue"]
		if key == "" {
			return nil, fmt.Errorf("%s delete requires table and deleteKey", dialect.Name)
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
			query := fmt.Sprintf("DELETE FROM %s WHERE %s = %s", table, sqlIdent(key, dialect), dialect.Placeholder(1))
			if returning := sqlReturningClause(in.Node.Parameters, dialect); returning != "" {
				rows, err := sqlQueryRows(ctx, db, query+" RETURNING "+returning, whereValue)
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
			output = append(output, sqlResultItem(result))
		}
		return dataplane.MainOutput(output), nil
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	output := make([]dataplane.Item, 0, len(items))
	for index := range items {
		whereClause, args := sqlWhereClause(in, in.Node.Parameters, dialect, items, index)
		query := fmt.Sprintf("DELETE FROM %s", table)
		if whereClause != "" {
			query += " WHERE " + whereClause
		}
		if returning := sqlReturningClause(in.Node.Parameters, dialect); returning != "" {
			rows, err := sqlQueryRows(ctx, db, query+" RETURNING "+returning, args...)
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
		output = append(output, sqlResultItem(result))
	}
	return dataplane.MainOutput(output), nil
}

func sqlSelect(ctx context.Context, db *sql.DB, in engine.ExecuteInput, dialect sqlDialect) (dataplane.Output, error) {
	table := sqlTableName(in.Node.Parameters, dialect)
	if table == "" {
		return nil, fmt.Errorf("%s select requires table", dialect.Name)
	}
	options := sqlOptions(in.Node.Parameters)
	columns := sqlSelectColumns(in.Node.Parameters, options, dialect)
	selectKeyword := "SELECT"
	if dialect.Name == "mysql" && boolParam(options, "selectDistinct", false) {
		selectKeyword = "SELECT DISTINCT"
	}
	query := fmt.Sprintf("%s %s FROM %s", selectKeyword, columns, table)
	args := []any{}
	if raw := strings.TrimSpace(stringParam(in.Node.Parameters, "where")); raw != "" {
		query += " WHERE " + raw
		args = sqlArgs(in, firstInput(in.InputData), 0)
	} else {
		whereClause, whereArgs := sqlWhereClause(in, in.Node.Parameters, dialect, firstInput(in.InputData), 0)
		if whereClause != "" {
			query += " WHERE " + whereClause
		}
		args = whereArgs
	}
	if sortClause := sqlSortClause(in.Node.Parameters, dialect); sortClause != "" {
		query += " " + sortClause
	}
	if !boolParam(in.Node.Parameters, "returnAll", boolParam(options, "returnAll", false)) {
		limit := intParam(in.Node.Parameters, "limit", intParam(options, "limit", 50))
		if limit <= 0 {
			limit = 50
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset := intParam(in.Node.Parameters, "offset", 0); offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}
	rows, err := sqlQueryRows(ctx, db, query, args...)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(rows), nil
}

func sqlQueryRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]dataplane.Item, error) {
	return sqlQueryRowsWithRunner(ctx, db, query, args...)
}

func sqlQueryRowsWithRunner(ctx context.Context, runner sqlRunner, query string, args ...any) ([]dataplane.Item, error) {
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
			item.JSON[column] = sqlValue(values[i])
		}
		output = append(output, item)
	}
	return output, rows.Err()
}

func sqlResultItem(result sql.Result) dataplane.Item {
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	return dataplane.Item{JSON: map[string]any{"rowsAffected": rowsAffected, "lastInsertId": lastInsertID}}
}

func sqlArgs(in engine.ExecuteInput, items []dataplane.Item, index int) []any {
	return sqlArgsWithExpected(in, items, index, 0)
}

func sqlArgsForQuery(in engine.ExecuteInput, items []dataplane.Item, index int, query string, dialect sqlDialect) []any {
	return sqlArgsWithExpected(in, items, index, sqlPlaceholderCount(query, dialect))
}

func sqlArgsWithExpected(in engine.ExecuteInput, items []dataplane.Item, index int, expected int) []any {
	options := sqlOptions(in.Node.Parameters)
	if raw, ok := options["queryReplacement"]; ok {
		if args := sqlArgsFromRawExpected(in, items, index, raw, expected); len(args) > 0 {
			return args
		}
	}
	raw, ok := in.Node.Parameters["queryParams"]
	if !ok {
		raw = in.Node.Parameters["parameters"]
	}
	return sqlArgsFromRawExpected(in, items, index, raw, expected)
}

func sqlArgsFromRaw(in engine.ExecuteInput, items []dataplane.Item, index int, raw any) []any {
	return sqlArgsFromRawExpected(in, items, index, raw, 0)
}

func sqlArgsFromRawExpected(in engine.ExecuteInput, items []dataplane.Item, index int, raw any, expected int) []any {
	if object, ok := raw.(map[string]any); ok {
		if values, ok := object["values"].([]any); ok {
			raw = values
		} else if values, ok := object["queryParams"].([]any); ok {
			raw = values
		}
	}
	switch values := raw.(type) {
	case nil:
		return nil
	case []any:
		if expected == 1 {
			return []any{resolveValue(in, items, index, values)}
		}
		return sqlResolveArgs(in, items, index, values)
	case []string:
		if expected == 1 {
			return []any{resolveValue(in, items, index, values)}
		}
		args := make([]any, 0, len(values))
		for _, value := range values {
			args = append(args, resolveValue(in, items, index, value))
		}
		return args
	case string:
		rawText := strings.TrimSpace(fmt.Sprint(resolveValue(in, items, index, values)))
		if rawText == "" {
			return nil
		}
		if expected == 1 && strings.HasPrefix(rawText, "[") && json.Valid([]byte(rawText)) {
			return []any{rawText}
		}
		var decoded []any
		if json.Unmarshal([]byte(rawText), &decoded) == nil {
			return sqlResolveArgs(in, items, index, decoded)
		}
		parts := splitCSV(rawText)
		args := make([]any, 0, len(parts))
		for _, value := range parts {
			args = append(args, resolveValue(in, items, index, value))
		}
		return args
	default:
		return []any{resolveValue(in, items, index, raw)}
	}
}

func sqlResolveArgs(in engine.ExecuteInput, items []dataplane.Item, index int, values []any) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			value = firstNonNil(object["value"], object["name"])
		}
		args = append(args, resolveValue(in, items, index, value))
	}
	return args
}

func sqlColumns(params map[string]any, item map[string]any) []string {
	if raw := sqlColumnsFromResourceMapper(params); len(raw) > 0 {
		return raw
	}
	if raw := stringParam(params, "columns"); raw != "" {
		return splitCSV(raw)
	}
	columns := make([]string, 0, len(item))
	for key := range item {
		columns = append(columns, key)
	}
	sortStrings(columns)
	return columns
}

func sqlMappedItem(in engine.ExecuteInput, items []dataplane.Item, index int, item map[string]any, params map[string]any) map[string]any {
	object, ok := params["columns"].(map[string]any)
	if !ok {
		return item
	}
	if strings.ToLower(fmt.Sprint(object["mappingMode"])) != "definebelow" {
		return item
	}
	valueObject, ok := rawObject(object["value"])
	if !ok || len(valueObject) == 0 {
		return item
	}
	mapped := make(map[string]any, len(valueObject))
	for key, value := range valueObject {
		mapped[key] = resolveValue(in, items, index, value)
	}
	return mapped
}

func sqlSelectColumns(params map[string]any, options map[string]any, dialect sqlDialect) string {
	if raw := stringListValue(options["outputColumns"]); raw != "" {
		return sqlIdentList(splitCSV(raw), dialect)
	}
	if raw := stringListValue(options["returnFields"]); raw != "" {
		return sqlIdentList(splitCSV(raw), dialect)
	}
	if raw := stringParam(params, "columns"); raw != "" {
		return sqlIdentList(splitCSV(raw), dialect)
	}
	if raw := stringParam(params, "returnFields"); raw != "" {
		return sqlIdentList(splitCSV(raw), dialect)
	}
	return "*"
}

func sqlOptions(params map[string]any) map[string]any {
	options := mergeObject(params["additionalFields"])
	for key, value := range mergeObject(params["options"]) {
		options[key] = value
	}
	return options
}

func sqlColumnsFromResourceMapper(params map[string]any) []string {
	object, ok := params["columns"].(map[string]any)
	if !ok {
		return nil
	}
	if strings.ToLower(fmt.Sprint(object["mappingMode"])) != "definebelow" {
		return nil
	}
	valueObject, ok := rawObject(object["value"])
	if !ok || len(valueObject) == 0 {
		return nil
	}
	columns := make([]string, 0, len(valueObject))
	for key := range valueObject {
		columns = append(columns, key)
	}
	sortStrings(columns)
	return columns
}

func sqlMatchingColumns(params map[string]any, fallback []string) []string {
	if object, ok := params["columns"].(map[string]any); ok {
		if columns := stringSliceValues(object["matchingColumns"]); len(columns) > 0 {
			return columns
		}
	}
	if columns := stringSliceValues(params["columns.matchingColumns"]); len(columns) > 0 {
		return columns
	}
	if columns := stringSliceValues(params["matchingColumns"]); len(columns) > 0 {
		return columns
	}
	if raw := stringParam(params, "updateKey", "whereKey"); raw != "" {
		return []string{raw}
	}
	if raw := stringParam(params, "columnToMatchOn"); raw != "" {
		return []string{raw}
	}
	return fallback
}

func sqlWhereClause(in engine.ExecuteInput, params map[string]any, dialect sqlDialect, items []dataplane.Item, index int) (string, []any) {
	if raw := strings.TrimSpace(stringParam(params, "where")); raw != "" {
		return raw, nil
	}
	object, ok := params["where"].(map[string]any)
	if !ok {
		return "", nil
	}
	entries := sqlConditionEntries(object["values"])
	if len(entries) == 0 {
		entries = sqlConditionEntries(object)
	}
	if len(entries) == 0 {
		return "", nil
	}
	combine := strings.ToUpper(stringParam(params, "combineConditions"))
	if combine != "OR" {
		combine = "AND"
	}
	clauses := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries))
	for _, entry := range entries {
		clause, clauseArgs := sqlConditionClause(in, entry, dialect, items, index)
		if clause == "" {
			continue
		}
		clauses = append(clauses, clause)
		args = append(args, clauseArgs...)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	if len(clauses) == 1 {
		return clauses[0], args
	}
	return "(" + strings.Join(clauses, " "+combine+" ") + ")", args
}

func sqlSortClause(params map[string]any, dialect sqlDialect) string {
	object, ok := params["sort"].(map[string]any)
	if !ok {
		return ""
	}
	entries := sqlConditionEntries(object["values"])
	if len(entries) == 0 {
		entries = sqlConditionEntries(object)
	}
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		column := resourceLocatorStringValue(entry["column"])
		if column == "" {
			continue
		}
		direction := strings.ToUpper(strings.TrimSpace(fmt.Sprint(entry["direction"])))
		if direction != "DESC" {
			direction = "ASC"
		}
		parts = append(parts, sqlIdent(column, dialect)+" "+direction)
	}
	if len(parts) == 0 {
		return ""
	}
	return "ORDER BY " + strings.Join(parts, ", ")
}

func sqlConditionEntries(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, entry := range typed {
			if object, ok := entry.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	case map[string]any:
		if values, ok := typed["values"]; ok {
			return sqlConditionEntries(values)
		}
		if values, ok := typed["entries"]; ok {
			return sqlConditionEntries(values)
		}
	}
	return nil
}

func sqlConditionClause(in engine.ExecuteInput, entry map[string]any, dialect sqlDialect, items []dataplane.Item, index int) (string, []any) {
	column := resourceLocatorStringValue(entry["column"])
	if column == "" {
		return "", nil
	}
	condition := strings.ToUpper(strings.TrimSpace(fmt.Sprint(entry["condition"])))
	switch condition {
	case "IS NULL", "IS NOT NULL":
		return sqlIdent(column, dialect) + " " + condition, nil
	}
	value := resolveValue(in, items, index, entry["value"])
	if raw, ok := entry["value"].(string); ok && strings.HasPrefix(raw, "=") {
		value = resolveValue(in, items, index, raw)
	}
	operator := conditionToSQL(condition)
	if operator == "" {
		operator = "="
	}
	return fmt.Sprintf("%s %s %s", sqlIdent(column, dialect), operator, dialect.Placeholder(1)), []any{value}
}

func conditionToSQL(condition string) string {
	switch condition {
	case "EQUAL", "EQ", "=", "EQUALS":
		return "="
	case "!=", "NOT EQUAL":
		return "!="
	case "LIKE":
		return "LIKE"
	case ">":
		return ">"
	case "<":
		return "<"
	case ">=":
		return ">="
	case "<=":
		return "<="
	default:
		return ""
	}
}

func stringListValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []string:
		return strings.Join(typed, ",")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, entry := range typed {
			if s := strings.TrimSpace(fmt.Sprint(entry)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	case map[string]any:
		if nested, ok := typed["value"]; ok {
			return stringListValue(nested)
		}
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringSliceValues(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, entry := range typed {
			if s := strings.TrimSpace(fmt.Sprint(entry)); s != "" {
				result = append(result, s)
			}
		}
		return result
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return splitCSV(typed)
	case map[string]any:
		if nested, ok := typed["value"]; ok {
			return stringSliceValues(nested)
		}
	}
	return nil
}

func resourceLocatorStringParam(params map[string]any, key string) string {
	return resourceLocatorStringValue(params[key])
}

func resourceLocatorStringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if nested, ok := typed["value"]; ok {
			return resourceLocatorStringValue(nested)
		}
		if nested, ok := typed["id"]; ok {
			return resourceLocatorStringValue(nested)
		}
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sqlReturningClause(params map[string]any, dialect sqlDialect) string {
	if dialect.Name != "postgres" {
		return ""
	}
	options := sqlOptions(params)
	raw := firstNonEmptyNode(stringParam(params, "returnFields"), stringListValue(options["outputColumns"]), stringListValue(options["returnFields"]))
	if raw == "" && boolParam(options, "returnId", false) {
		raw = "id"
	}
	if raw == "" {
		return ""
	}
	if raw == "*" {
		return "*"
	}
	return sqlIdentList(splitCSV(raw), dialect)
}

func sqlTableName(params map[string]any, dialect sqlDialect) string {
	table := resourceLocatorStringParam(params, "table")
	if table == "" {
		return ""
	}
	schema := resourceLocatorStringParam(params, "schema")
	if schema == "" {
		return sqlIdent(table, dialect)
	}
	return sqlIdent(schema, dialect) + "." + sqlIdent(table, dialect)
}

func sqlReturnsRows(query string) bool {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "select", "show", "describe", "desc", "with", "explain", "pragma":
		return true
	default:
		return false
	}
}

func sqlStatements(query string) []string {
	parts := strings.Split(query, ";")
	if len(parts) == 1 {
		return []string{strings.TrimSpace(query)}
	}
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func sqlStatementArgs(statement string, dialect sqlDialect, args []any, offset int) ([]any, int) {
	if len(args) == 0 {
		return nil, 0
	}
	if dialect.Name == "postgres" {
		maxPlaceholder := 0
		for i := 0; i < len(statement)-1; i++ {
			if statement[i] == '$' && statement[i+1] >= '0' && statement[i+1] <= '9' {
				value := 0
				for i++; i < len(statement) && statement[i] >= '0' && statement[i] <= '9'; i++ {
					value = value*10 + int(statement[i]-'0')
				}
				i--
				if value > maxPlaceholder {
					maxPlaceholder = value
				}
			}
		}
		if maxPlaceholder == 0 {
			return nil, 0
		}
		if maxPlaceholder > len(args) {
			maxPlaceholder = len(args)
		}
		return args[:maxPlaceholder], 0
	}
	count := strings.Count(statement, "?")
	if count == 0 || offset >= len(args) {
		return nil, 0
	}
	end := offset + count
	if end > len(args) {
		end = len(args)
	}
	return args[offset:end], end - offset
}

func sqlPlaceholderCount(query string, dialect sqlDialect) int {
	if dialect.Name != "postgres" {
		return strings.Count(query, "?")
	}
	maxPlaceholder := 0
	for i := 0; i < len(query)-1; i++ {
		if query[i] != '$' || query[i+1] < '0' || query[i+1] > '9' {
			continue
		}
		value := 0
		for i++; i < len(query) && query[i] >= '0' && query[i] <= '9'; i++ {
			value = value*10 + int(query[i]-'0')
		}
		i--
		if value > maxPlaceholder {
			maxPlaceholder = value
		}
	}
	return maxPlaceholder
}

func sqlIdent(value string, dialect sqlDialect) string {
	return dialect.Quote + strings.ReplaceAll(value, dialect.Quote, dialect.Quote+dialect.Quote) + dialect.Quote
}

func sqlIdentList(values []string, dialect sqlDialect) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, sqlIdent(value, dialect))
	}
	return strings.Join(quoted, ", ")
}

func sqlValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return value
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
