package calibrate

import (
	"strings"
	"testing"
)

func TestDataExpression(t *testing.T) {
	domain := Archetypes()[0]

	tests := []struct {
		col      ColumnDef
		contains string
	}{
		{ColumnDef{Name: "name", Type: "VARCHAR(100)"}, "name_"},
		{ColumnDef{Name: "status", Type: "VARCHAR(20)"}, "active"},   // skewed distribution
		{ColumnDef{Name: "amount", Type: "NUMERIC(10,2)"}, "power"},  // skewed numeric
		{ColumnDef{Name: "is_active", Type: "BOOLEAN"}, "random"},
		{ColumnDef{Name: "created_at", Type: "TIMESTAMPTZ"}, "730"},  // 2-year range
		{ColumnDef{Name: "payment_date", Type: "DATE"}, "CURRENT_DATE"},
		{ColumnDef{Name: "quantity", Type: "INT"}, "random"},
		{ColumnDef{Name: "clinical_data", Type: "JSONB"}, "jsonb_build_object"},
	}

	for _, tt := range tests {
		expr := dataExpression(tt.col, 1000, 1000, domain, domain.Tables[0])
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
		{"status", "active"},
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
	domain := Archetypes()[0] // cash_accounting

	// Find a table that has FKs (child) and one that doesn't (root)
	var childTable, rootTable TableDef
	for _, tbl := range domain.Tables {
		if IsChildTable(tbl, domain) {
			childTable = tbl
		} else {
			rootTable = tbl
		}
		if childTable.Name != "" && rootTable.Name != "" {
			break
		}
	}

	if childTable.Name == "" {
		t.Fatal("no child table found")
	}
	if rootTable.Name == "" {
		t.Fatal("no root table found")
	}

	if !IsChildTable(childTable, domain) {
		t.Errorf("%s should be a child table", childTable.Name)
	}
	if IsChildTable(rootTable, domain) {
		t.Errorf("%s should not be a child table", rootTable.Name)
	}
}

func TestTopologicalSort_NoCycle(t *testing.T) {
	domain := Archetypes()[0]
	sorted := TopologicalSort(domain)
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

	sorted := TopologicalSort(domain)
	if len(sorted) != 1 {
		t.Errorf("expected 1 table, got %d", len(sorted))
	}
}

func TestGenerateInsertSQL(t *testing.T) {
	domain := Archetypes()[0]
	cfg := DefaultConfig()
	cfg.RowsPerTable = 100
	dg := &DataGenerator{db: nil, cfg: cfg}

	firstTable := domain.Tables[0]
	sql := dg.generateInsertSQL("test_schema", firstTable, domain)
	if !strings.Contains(sql, "INSERT INTO test_schema."+firstTable.Name) {
		t.Errorf("expected INSERT INTO test_schema.%s, got: %s", firstTable.Name, sql[:min(80, len(sql))])
	}
	if !strings.Contains(sql, "generate_series") {
		t.Errorf("expected generate_series, got: %s", sql)
	}
	// Should not include id column (serial)
	if strings.Contains(sql, "(id,") {
		t.Error("should not include serial id column in INSERT")
	}
}

func TestGenerateInsertSQL_ChildTable(t *testing.T) {
	domain := Archetypes()[0]
	cfg := DefaultConfig()
	cfg.RowsPerTable = 100
	dg := &DataGenerator{db: nil, cfg: cfg}

	// Find a child table and verify it gets more rows than baseRows
	for _, tbl := range domain.Tables {
		if IsChildTable(tbl, domain) && !HasCompositeUnique(tbl, domain) {
			sql := dg.generateInsertSQL("test_schema", tbl, domain)
			// Child tables get multiplier > 1, so rows > 100
			if strings.Contains(sql, "generate_series(1, 100)") {
				t.Errorf("child table %s should have more than base rows, got: %s", tbl.Name, sql[:min(100, len(sql))])
			}
			return
		}
	}
	t.Skip("no suitable child table found")
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
