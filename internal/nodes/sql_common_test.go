package nodes

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/metadata"
)

var sqliteTestDialect = sqlDialect{Name: "sqlite", Quote: `"`, Placeholder: func(int) string { return "?" }}

func TestSQLRuntimeSupportsOriginalOperations(t *testing.T) {
	t.Parallel()

	want := map[string][]string{
		"database": {"deleteTable", "executeQuery", "insert", "select", "update", "upsert"},
	}
	for _, nodeType := range []string{"n8n-nodes-base.mySql", "n8n-nodes-base.postgres"} {
		got := originalSQLOperations(t, nodeType)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s original operations changed or runtime coverage is stale\n got: %#v\nwant: %#v", nodeType, got, want)
		}
	}
}

func TestSQLExecuteQueryUsesOfficialQueryReplacementOption(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)
	seedSQLTestRows(t, db, 3)

	out, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"operation": "executeQuery",
		"query":     `SELECT name FROM "items" WHERE id = ?`,
		"options": map[string]any{
			"queryReplacement": "2",
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}), sqliteTestDialect)
	if err != nil {
		t.Fatalf("execute query: %v", err)
	}
	if got := out[0][0].JSON["name"]; got != "item-2" {
		t.Fatalf("unexpected queryReplacement result: %#v", out[0][0].JSON)
	}
}

func TestSQLExecuteQueryRunsMultipleStatementsWithQueryReplacement(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)

	out, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"operation": "executeQuery",
		"query":     `CREATE TABLE "{{ $json.tableName }}" (id TEXT); INSERT INTO "{{ $json.tableName }}" (id) VALUES (?)`,
		"options": map[string]any{
			"queryReplacement": "abc",
		},
	}, []dataplane.Item{{JSON: map[string]any{"tableName": "invoice-1"}}}), sqliteTestDialect)
	if err != nil {
		t.Fatalf("execute multi statement query: %v", err)
	}
	if got := len(out[0]); got != 2 {
		t.Fatalf("expected one result per statement, got %d", got)
	}

	rows, err := sqlQueryRows(context.Background(), db, `SELECT id FROM "invoice-1"`)
	if err != nil {
		t.Fatalf("select inserted row: %v", err)
	}
	if got := rows[0].JSON["id"]; got != "abc" {
		t.Fatalf("unexpected inserted row: %#v", rows[0].JSON)
	}
}

func TestSQLStatementArgsForPostgresMultiStatementQuery(t *testing.T) {
	t.Parallel()

	args := []any{"order-1", `[{"sku":"sku-1","pret_total":12.34}]`}

	deleteArgs, used := sqlStatementArgs(`DELETE FROM manifests.pallets WHERE orderid = $1`, postgresDialect, args, 0)
	if used != 0 || !reflect.DeepEqual(deleteArgs, []any{"order-1"}) {
		t.Fatalf("unexpected delete args: used=%d args=%#v", used, deleteArgs)
	}

	insertArgs, used := sqlStatementArgs(`INSERT INTO manifests.pallets SELECT $1::varchar, a FROM json_array_elements($2::json) AS a`, postgresDialect, args, 0)
	if used != 0 || !reflect.DeepEqual(insertArgs, args) {
		t.Fatalf("unexpected insert args: used=%d args=%#v", used, insertArgs)
	}
}

func TestSQLQueryReplacementKeepsJSONArgumentTogether(t *testing.T) {
	t.Parallel()

	in := testInput(map[string]any{
		"options": map[string]any{
			"queryReplacement": `order-1, [{"sku":"sku-1","pret_total":12.34},{"sku":"sku-2","pret_total":56.78}]`,
		},
	}, []dataplane.Item{{JSON: map[string]any{}}})

	args := sqlArgs(in, firstInput(in.InputData), 0)
	want := []any{"order-1", `[{"sku":"sku-1","pret_total":12.34},{"sku":"sku-2","pret_total":56.78}]`}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected query args: %#v", args)
	}
}

func TestSQLQueryReplacementKeepsSinglePlaceholderJSONBatchWhole(t *testing.T) {
	t.Parallel()

	batch := []any{
		map[string]any{"ASIN": "B0FSKD4JMD", "Product_SKU": "220526-00047-1385179"},
		map[string]any{"ASIN": "B0G5YJGDZS", "Product_SKU": "020626-00047-1403243"},
	}
	in := testInput(map[string]any{
		"options": map[string]any{
			"queryReplacement": `{{ JSON.stringify($json.batch) }}`,
		},
	}, []dataplane.Item{{JSON: map[string]any{"batch": batch}}})

	args := sqlArgsForQuery(in, firstInput(in.InputData), 0, `SELECT * FROM json_to_recordset($1)`, postgresDialect)
	if len(args) != 1 {
		t.Fatalf("expected one whole JSON argument, got %d: %#v", len(args), args)
	}
	arg, ok := args[0].(string)
	if !ok {
		t.Fatalf("expected JSON string argument, got %T: %#v", args[0], args[0])
	}
	if !strings.Contains(arg, `"B0FSKD4JMD"`) || !strings.Contains(arg, `"B0G5YJGDZS"`) {
		t.Fatalf("JSON argument lost batch items: %s", arg)
	}
}

func TestSQLMissingOperationDefaultsToOfficialInsertWhenStructured(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	out, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"table": "items",
		"columns": map[string]any{
			"mappingMode": "defineBelow",
			"value": map[string]any{
				"id":   1,
				"name": "inserted",
			},
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}), sqliteTestDialect)
	if err != nil {
		t.Fatalf("default insert: %v", err)
	}
	if got := out[0][0].JSON["rowsAffected"]; got != int64(1) {
		t.Fatalf("default operation should insert one row, got %#v", out[0][0].JSON)
	}
}

func TestSQLMissingOperationKeepsLegacyExecuteQueryWhenQueryExists(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)
	seedSQLTestRows(t, db, 1)

	out, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"query": `SELECT name FROM "items" WHERE id = ?`,
		"options": map[string]any{
			"queryReplacement": "1",
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}), sqliteTestDialect)
	if err != nil {
		t.Fatalf("legacy query default: %v", err)
	}
	if got := out[0][0].JSON["name"]; got != "item-1" {
		t.Fatalf("missing operation with query should execute query, got %#v", out[0][0].JSON)
	}
}

func TestSQLSelectDefaultsToOfficialLimitUnlessReturnAll(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)
	seedSQLTestRows(t, db, 55)

	limited, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"operation": "select",
		"table":     "items",
	}, []dataplane.Item{{JSON: map[string]any{}}}), sqliteTestDialect)
	if err != nil {
		t.Fatalf("select limited: %v", err)
	}
	if got := len(limited[0]); got != 50 {
		t.Fatalf("official select default should return 50 rows, got %d", got)
	}

	allRows, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"operation": "select",
		"table":     "items",
		"returnAll": true,
	}, []dataplane.Item{{JSON: map[string]any{}}}), sqliteTestDialect)
	if err != nil {
		t.Fatalf("select returnAll: %v", err)
	}
	if got := len(allRows[0]); got != 55 {
		t.Fatalf("returnAll should return every row, got %d", got)
	}
}

func TestSQLInsertSupportsOfficialPostgresSkipOnConflict(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)
	seedSQLTestRows(t, db, 1)

	out, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"operation": "insert",
		"table":     "items",
		"columns": map[string]any{
			"mappingMode": "defineBelow",
			"value": map[string]any{
				"id":   1,
				"name": "duplicate",
			},
		},
		"options": map[string]any{
			"skipOnConflict": true,
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}), sqlDialect{Name: "postgres", Quote: `"`, Placeholder: func(index int) string { return "$" + strconv.Itoa(index) }})
	if err != nil {
		t.Fatalf("insert skipOnConflict: %v", err)
	}
	if got := out[0][0].JSON["rowsAffected"]; got != int64(0) {
		t.Fatalf("duplicate insert should be skipped, got %#v", out[0][0].JSON)
	}
}

func TestSQLUpsertUsesDialectSpecificConflictSyntax(t *testing.T) {
	t.Parallel()

	mysqlQuery, mysqlUpdates, err := sqlUpsertQuery("`items`", []string{"id", "name", "slug"}, []string{"id"}, mysqlDialect)
	if err != nil {
		t.Fatalf("mysql upsert query: %v", err)
	}
	if want := "INSERT INTO `items` (`id`, `name`, `slug`) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE `name` = ?, `slug` = ?"; mysqlQuery != want {
		t.Fatalf("unexpected mysql upsert query:\nwant %s\n got %s", want, mysqlQuery)
	}
	if strings.Join(mysqlUpdates, ",") != "name,slug" {
		t.Fatalf("mysql update columns should exclude match column, got %#v", mysqlUpdates)
	}

	postgresQuery, postgresUpdates, err := sqlUpsertQuery(`"items"`, []string{"id", "name", "slug"}, []string{"id"}, postgresDialect)
	if err != nil {
		t.Fatalf("postgres upsert query: %v", err)
	}
	if want := `INSERT INTO "items" ("id", "name", "slug") VALUES ($1, $2, $3) ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name", "slug" = EXCLUDED."slug"`; postgresQuery != want {
		t.Fatalf("unexpected postgres upsert query:\nwant %s\n got %s", want, postgresQuery)
	}
	if len(postgresUpdates) != 0 {
		t.Fatalf("postgres should not need duplicate update args, got %#v", postgresUpdates)
	}
}

func TestSQLSelectSupportsOfficialMySQLSelectDistinctOption(t *testing.T) {
	t.Parallel()

	db := openSQLiteTestDB(t)
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for index, name := range []string{"same", "same", "other"} {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO items (id, name) VALUES (?, ?)`, index+1, name); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}

	out, err := executeSQLNode(context.Background(), db, testInput(map[string]any{
		"operation": "select",
		"table":     "items",
		"columns":   "name",
		"returnAll": true,
		"options": map[string]any{
			"selectDistinct": true,
		},
	}, []dataplane.Item{{JSON: map[string]any{}}}), mysqlDialect)
	if err != nil {
		t.Fatalf("mysql select distinct: %v", err)
	}
	if got := len(out[0]); got != 2 {
		t.Fatalf("SELECT DISTINCT should collapse duplicate names, got %d rows: %#v", got, out[0])
	}
}

func openSQLiteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedSQLTestRows(t *testing.T, db *sql.DB, count int) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for i := 1; i <= count; i++ {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO items (id, name) VALUES (?, ?)`, i, "item-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}
}

func originalSQLOperations(t *testing.T, nodeType string) map[string][]string {
	t.Helper()

	node, ok := metadata.NodeTypeByName(nodeType, []string{nodeType})
	if !ok || node.Raw == nil {
		t.Fatalf("%s original metadata is unavailable", nodeType)
	}
	properties, ok := node.Raw["properties"].([]any)
	if !ok {
		t.Fatalf("%s metadata has no properties", nodeType)
	}
	result := map[string][]string{}
	for _, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok || prop["name"] != "operation" {
			continue
		}
		display, _ := prop["displayOptions"].(map[string]any)
		show, _ := display["show"].(map[string]any)
		options, _ := prop["options"].([]any)
		for _, resource := range sqlStringList(show["resource"]) {
			for _, rawOption := range options {
				option, ok := rawOption.(map[string]any)
				if !ok {
					continue
				}
				if value, ok := option["value"].(string); ok {
					result[resource] = append(result[resource], value)
				}
			}
		}
	}
	for resource := range result {
		sort.Strings(result[resource])
	}
	return result
}

func sqlStringList(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
