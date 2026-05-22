package mysqldb

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

func testMySQLDomain() calibrate.Domain {
	return MapPgTypesToMySQL(calibrate.Archetypes()[0]) // cash_accounting
}

func TestMySQLTemplates_NonEmpty(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)
	if len(tmpls) == 0 {
		t.Fatal("expected non-empty templates")
	}
	t.Logf("Generated %d templates for %s domain", len(tmpls), d.Name)
}

func TestMySQLTemplates_MinimumCount(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)
	// With the expanded coverage, we should have a large number of templates
	if len(tmpls) < 100 {
		t.Errorf("expected at least 100 templates, got %d", len(tmpls))
	}
}

func TestMySQLTemplates_AllGenerateValidSQL(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)
	rng := rand.New(rand.NewSource(42))

	for i, tmpl := range tmpls {
		sql := tmpl.Gen(rng)
		if sql == "" {
			t.Errorf("template %d (%s) generated empty SQL", i, tmpl.QueryType)
		}
		// Basic sanity: should start with a SQL keyword
		upper := strings.ToUpper(strings.TrimSpace(sql))
		validStarts := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "REPLACE", "WITH"}
		valid := false
		for _, s := range validStarts {
			if strings.HasPrefix(upper, s) {
				valid = true
				break
			}
		}
		if !valid {
			t.Errorf("template %d (%s) generated invalid SQL: %s", i, tmpl.QueryType, sql[:min(80, len(sql))])
		}
	}
}

func TestMySQLTemplates_NoPgSyntax(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)
	rng := rand.New(rand.NewSource(42))

	pgPatterns := []string{"::text", "::int", "random()", "now()", "generate_series", "RETURNING", "LATERAL"}
	for _, tmpl := range tmpls {
		sql := tmpl.Gen(rng)
		for _, p := range pgPatterns {
			if strings.Contains(sql, p) {
				t.Errorf("template %s generated PG syntax %q: %s", tmpl.QueryType, p, sql[:min(80, len(sql))])
			}
		}
	}
}

func TestMySQLTemplates_CoverAllAntipatterns(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)

	rulesSeen := make(map[string]bool)
	for _, tmpl := range tmpls {
		for _, r := range tmpl.Rules {
			rulesSeen[r] = true
		}
	}

	requiredRules := []string{
		"select-star", "non-sargable", "missing-predicate", "unbounded-sort",
		"distinct-dedup", "group-by-fanout", "like-leading-wildcard", "large-offset",
		"boolean-nesting", "subquery-nesting", "case-expression", "missing-where-clause",
		"join", "outer-join", "set-operation", "for-update-lock",
		"window-function", "window-no-partition-extra", "cte", "recursive-cte",
		"correlated-subquery", "null-coalesce-in-predicate", "null-check-chain",
		"expensive-function", "volatile-function", "ddl-statement", "cascade-drop",
		"implicit-cast-in-predicate", "large-in-list",
	}

	for _, rule := range requiredRules {
		if !rulesSeen[rule] {
			t.Errorf("missing template for rule: %s", rule)
		}
	}
}

func TestMySQLTemplates_QueryTypes(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)

	typesSeen := make(map[string]bool)
	for _, tmpl := range tmpls {
		typesSeen[tmpl.QueryType] = true
	}

	expectedTypes := []string{
		"select_star", "non_sargable", "missing_predicate", "unbounded_sort",
		"distinct", "group_by", "like_wildcard", "large_offset",
		"boolean_nesting", "subquery", "case_expr", "update_no_where", "delete_no_where",
		"join", "left_join", "right_join", "union",
		"correlated_subquery", "derived_table", "having", "rollup",
		"coalesce_predicate", "ifnull", "regexp", "large_in_list",
		"for_update", "lock_share",
		"window_func", "window_no_partition", "cte", "recursive_cte",
		"insert_on_dup", "replace_into", "insert_ignore",
		"group_concat", "find_in_set", "implicit_cast",
		"create_proc", "create_func",
		"volatile_func", "volatile_rand",
		"create_table", "alter_table", "drop_table",
		"create_myisam", "create_memory", "create_archive",
		"alter_engine", "straight_join",
	}

	for _, qt := range expectedTypes {
		if !typesSeen[qt] {
			t.Errorf("missing query type: %s", qt)
		}
	}
}

func TestMySQLTemplates_JoinTemplatesExist(t *testing.T) {
	d := testMySQLDomain()
	if len(d.ForeignKeys) == 0 {
		t.Skip("domain has no foreign keys")
	}
	tmpls := mysqlTemplates(d)

	joinTypes := map[string]bool{}
	for _, tmpl := range tmpls {
		if strings.Contains(tmpl.QueryType, "join") {
			joinTypes[tmpl.QueryType] = true
		}
	}

	for _, expected := range []string{"join", "left_join", "right_join", "multi_join", "straight_join"} {
		if !joinTypes[expected] {
			t.Errorf("missing join template: %s", expected)
		}
	}
}

func TestMySQLTemplates_EngineTemplatesExist(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)

	engineTypes := map[string]bool{}
	for _, tmpl := range tmpls {
		if strings.HasPrefix(tmpl.QueryType, "create_") {
			engineTypes[tmpl.QueryType] = true
		}
	}

	for _, expected := range []string{"create_myisam", "create_memory", "create_archive", "create_table"} {
		if !engineTypes[expected] {
			t.Errorf("missing engine template: %s", expected)
		}
	}
}

func TestMySQLTemplates_StoredProcFuncTemplates(t *testing.T) {
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)

	found := map[string]bool{}
	for _, tmpl := range tmpls {
		found[tmpl.QueryType] = true
	}

	if !found["create_proc"] {
		t.Error("missing create_proc template")
	}
	if !found["create_func"] {
		t.Error("missing create_func template")
	}
}

func TestMySQLTemplates_JSONTemplates(t *testing.T) {
	// Use a domain that has JSON columns
	d := testMySQLDomain()
	tmpls := mysqlTemplates(d)

	jsonTypes := map[string]bool{}
	for _, tmpl := range tmpls {
		if strings.HasPrefix(tmpl.QueryType, "json_") {
			jsonTypes[tmpl.QueryType] = true
		}
	}

	// The cash_accounting domain should have JSON columns after type mapping
	hasJSON := false
	for _, t := range d.Tables {
		for _, c := range t.Columns {
			if c.Type == "JSON" {
				hasJSON = true
				break
			}
		}
	}
	if hasJSON {
		for _, expected := range []string{"json_extract", "json_arrow", "json_contains"} {
			if !jsonTypes[expected] {
				t.Errorf("missing JSON template: %s", expected)
			}
		}
	}
}

func TestColLiteral_TypeSafety(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	tests := []struct {
		colType  string
		contains string
	}{
		{"INT", ""},          // numeric, no quotes
		{"BIGINT", ""},       // numeric
		{"DECIMAL(10,2)", ""}, // numeric
		{"DATE", "2025"},
		{"DATETIME", "2025"},
		{"TINYINT(1)", "1"},
		{"JSON", "{"},
		{"VARCHAR(100)", "value_"},
		{"TEXT", "value_"},
	}
	for _, tt := range tests {
		col := calibrate.ColumnDef{Name: "test", Type: tt.colType}
		lit := colLiteral(col, rng)
		if tt.contains != "" && !strings.Contains(lit, tt.contains) {
			t.Errorf("colLiteral(%s) = %q, expected to contain %q", tt.colType, lit, tt.contains)
		}
		if lit == "" {
			t.Errorf("colLiteral(%s) returned empty string", tt.colType)
		}
	}
}

func TestNewMySQLQueryGenerator(t *testing.T) {
	qg := NewMySQLQueryGenerator(42)
	d := testMySQLDomain()
	queries := qg.GenerateQueries(d, 1, 50)
	if len(queries) != 50 {
		t.Errorf("expected 50 queries, got %d", len(queries))
	}
	for i, q := range queries {
		if q.SQL == "" {
			t.Errorf("query %d has empty SQL", i)
		}
		if q.FamilyID != 1 {
			t.Errorf("query %d: familyID = %d, want 1", i, q.FamilyID)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
