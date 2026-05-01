package calibrate

import (
	"strings"
	"testing"
)

func TestDataExpression(t *testing.T) {
	domain := Archetypes()[0] // ecommerce

	tests := []struct {
		col      ColumnDef
		contains string
	}{
		{ColumnDef{Name: "email", Type: "VARCHAR(255)"}, "@example.com"},
		{ColumnDef{Name: "name", Type: "VARCHAR(100)"}, "name_"},
		{ColumnDef{Name: "status", Type: "VARCHAR(20)"}, "ARRAY"},
		{ColumnDef{Name: "price", Type: "NUMERIC(10,2)"}, "random"},
		{ColumnDef{Name: "is_active", Type: "BOOLEAN"}, "random"},
		{ColumnDef{Name: "created_at", Type: "TIMESTAMPTZ"}, "interval"},
		{ColumnDef{Name: "hire_date", Type: "DATE"}, "CURRENT_DATE"},
		{ColumnDef{Name: "id", Type: "INT"}, "random"},
		{ColumnDef{Name: "properties", Type: "JSONB"}, "jsonb_build_object"},
	}

	for _, tt := range tests {
		expr := dataExpression(tt.col, 1000, domain, domain.Tables[0])
		if !strings.Contains(expr, tt.contains) {
			t.Errorf("dataExpression(%s, %s) = %q, expected to contain %q",
				tt.col.Name, tt.col.Type, expr, tt.contains)
		}
	}
}

func TestTextExpression(t *testing.T) {
	tests := []struct {
		colName  string
		contains string
	}{
		{"email", "@example.com"},
		{"name", "name_"},
		{"title", "name_"},
		{"status", "ARRAY"},
		{"url", "https://"},
		{"country", "ARRAY"},
		{"type", "ARRAY"},
		{"slug", "slug-"},
		{"sku", "SKU-"},
		{"role", "ARRAY"},
		{"source", "ARRAY"},
		{"bio", "Lorem"},
		{"body", "Lorem"},
		{"location", "ARRAY"},
		{"device_type", "ARRAY"},
		{"section", "ARRAY"},
		{"external_id", "ext_"},
		{"unknown_col", "value_"},
	}

	for _, tt := range tests {
		expr := textExpression(tt.colName, 1000)
		if !strings.Contains(expr, tt.contains) {
			t.Errorf("textExpression(%q) = %q, expected to contain %q",
				tt.colName, expr, tt.contains)
		}
	}
}

func TestIsChildTable(t *testing.T) {
	domain := Archetypes()[0] // ecommerce

	// orders is a child (has FK to users)
	ordersTable := domain.Tables[3]
	if ordersTable.Name != "orders" {
		t.Fatalf("expected orders table, got %s", ordersTable.Name)
	}
	if !isChildTable(ordersTable, domain) {
		t.Error("orders should be a child table")
	}

	// users is not a child (no FK from it to others, only to it)
	usersTable := domain.Tables[0]
	if usersTable.Name != "users" {
		t.Fatalf("expected users table, got %s", usersTable.Name)
	}
	if isChildTable(usersTable, domain) {
		t.Error("users should not be a child table")
	}
}

func TestTopologicalSort_NoCycle(t *testing.T) {
	domain := Archetypes()[0]
	sorted := topologicalSort(domain)
	if len(sorted) != len(domain.Tables) {
		t.Errorf("expected %d tables, got %d", len(domain.Tables), len(sorted))
	}
}

func TestTopologicalSort_SelfReference(t *testing.T) {
	domain := Domain{
		Name: "selfref",
		Tables: []TableDef{
			{Name: "nodes", Columns: []ColumnDef{{Name: "id", Type: "SERIAL", IsSerial: true}, {Name: "parent_id", Type: "INT"}}},
		},
		ForeignKeys: []FKDef{
			{Name: "fk_self", Table: "nodes", Column: "parent_id", RefTable: "nodes", RefColumn: "id"},
		},
	}

	sorted := topologicalSort(domain)
	if len(sorted) != 1 {
		t.Errorf("expected 1 table, got %d", len(sorted))
	}
}

func TestGenerateInsertSQL(t *testing.T) {
	domain := Archetypes()[0]
	cfg := DefaultConfig()
	cfg.RowsPerTable = 100
	dg := &DataGenerator{db: nil, cfg: cfg}

	sql := dg.generateInsertSQL("test_schema", domain.Tables[0], domain)
	if !strings.Contains(sql, "INSERT INTO test_schema.users") {
		t.Error("expected INSERT INTO statement")
	}
	if !strings.Contains(sql, "generate_series(1, 100)") {
		t.Errorf("expected generate_series with 100 rows, got: %s", sql)
	}
	// Should not include id column (serial)
	if strings.Contains(sql, "(id,") || strings.HasPrefix(sql, "INSERT INTO test_schema.users (id") {
		t.Error("should not include serial id column in INSERT")
	}
}

func TestGenerateInsertSQL_ChildTable(t *testing.T) {
	domain := Archetypes()[0]
	cfg := DefaultConfig()
	cfg.RowsPerTable = 100
	dg := &DataGenerator{db: nil, cfg: cfg}

	// order_items is a child table — should have more rows
	sql := dg.generateInsertSQL("test_schema", domain.Tables[4], domain)
	if !strings.Contains(sql, "generate_series(1, 300)") {
		t.Errorf("child table should have 3x rows, got: %s", sql)
	}
}

func TestParseArray(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"{}", nil},
		{"{a}", []string{"a"}},
		{"{a,b,c}", []string{"a", "b", "c"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := parseArray(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseArray(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseArray(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParseExplainJSON(t *testing.T) {
	json := `[{"Plan": {"Node Type": "Seq Scan", "Total Cost": 42.5, "Startup Cost": 0.1, "Actual Total Time": 1.234, "Plan Rows": 100, "Actual Rows": 95, "Shared Hit Blocks": 10, "Shared Read Blocks": 5}}]`

	result, err := parseExplainJSON([]byte(json))
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCost != 42.5 {
		t.Errorf("TotalCost = %f, want 42.5", result.TotalCost)
	}
	if result.StartupCost != 0.1 {
		t.Errorf("StartupCost = %f, want 0.1", result.StartupCost)
	}
	if result.ActualTimeMs != 1.234 {
		t.Errorf("ActualTimeMs = %f, want 1.234", result.ActualTimeMs)
	}
	if result.RowsPlanned != 100 {
		t.Errorf("RowsPlanned = %d, want 100", result.RowsPlanned)
	}
	if result.RowsActual != 95 {
		t.Errorf("RowsActual = %d, want 95", result.RowsActual)
	}
	if result.SharedHitBlocks != 10 {
		t.Errorf("SharedHitBlocks = %d, want 10", result.SharedHitBlocks)
	}
	if result.SharedReadBlocks != 5 {
		t.Errorf("SharedReadBlocks = %d, want 5", result.SharedReadBlocks)
	}
}

func TestParseExplainJSON_Empty(t *testing.T) {
	_, err := parseExplainJSON([]byte("[]"))
	if err == nil {
		t.Error("expected error for empty EXPLAIN output")
	}
}

func TestParseExplainJSON_Invalid(t *testing.T) {
	_, err := parseExplainJSON([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
