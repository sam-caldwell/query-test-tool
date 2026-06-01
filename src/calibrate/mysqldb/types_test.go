package mysqldb

import (
	"testing"

	"github.com/sam-caldwell/query-test-tool/src/calibrate"
)

func TestMapColumnType(t *testing.T) {
	tests := []struct {
		pgType    string
		wantMySQL string
	}{
		{"SERIAL", "INT AUTO_INCREMENT"},
		{"BIGSERIAL", "BIGINT AUTO_INCREMENT"},
		{"TIMESTAMPTZ", "DATETIME"},
		{"TIMESTAMP", "DATETIME"},
		{"JSONB", "JSON"},
		{"BOOLEAN", "TINYINT(1)"},
		{"TEXT", "TEXT"},
		{"DATE", "DATE"},
		{"SMALLINT", "SMALLINT"},
		{"INT", "INT"},
		{"BIGINT", "BIGINT"},
		{"NUMERIC(10,2)", "DECIMAL(10,2)"},
		{"NUMERIC(14,2)", "DECIMAL(14,2)"},
		{"NUMERIC(3,2)", "DECIMAL(3,2)"},
		{"VARCHAR(100)", "VARCHAR(100)"},
		{"VARCHAR(255)", "VARCHAR(255)"},
	}

	for _, tt := range tests {
		got := mapColumnType(tt.pgType)
		if got != tt.wantMySQL {
			t.Errorf("mapColumnType(%q) = %q, want %q", tt.pgType, got, tt.wantMySQL)
		}
	}
}

func TestIsAutoIncrement(t *testing.T) {
	tests := []struct {
		typ  string
		want bool
	}{
		{"INT AUTO_INCREMENT", true},
		{"BIGINT AUTO_INCREMENT", true},
		{"INT", false},
		{"SERIAL", false},
		{"VARCHAR(100)", false},
	}

	for _, tt := range tests {
		got := IsAutoIncrement(tt.typ)
		if got != tt.want {
			t.Errorf("IsAutoIncrement(%q) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestMapPgTypesToMySQL(t *testing.T) {
	domain := calibrate.Domain{
		Name: "test",
		Tables: []calibrate.TableDef{
			{
				Name: "users",
				Columns: []calibrate.ColumnDef{
					{Name: "id", Type: "SERIAL", IsSerial: true},
					{Name: "name", Type: "VARCHAR(100)", NotNull: true},
					{Name: "email", Type: "TEXT"},
					{Name: "balance", Type: "NUMERIC(10,2)"},
					{Name: "is_active", Type: "BOOLEAN"},
					{Name: "created_at", Type: "TIMESTAMPTZ"},
					{Name: "metadata", Type: "JSONB"},
				},
			},
		},
		Indexes: []calibrate.IndexDef{
			{Name: "idx_users_email", Table: "users", Columns: []string{"email"}, Unique: true},
		},
	}

	mapped := MapPgTypesToMySQL(domain)

	// Verify types were converted
	cols := mapped.Tables[0].Columns
	expected := []string{
		"INT AUTO_INCREMENT", "VARCHAR(100)", "TEXT", "DECIMAL(10,2)",
		"TINYINT(1)", "DATETIME", "JSON",
	}
	for i, want := range expected {
		if cols[i].Type != want {
			t.Errorf("column %s: got type %q, want %q", cols[i].Name, cols[i].Type, want)
		}
	}

	// Verify original domain was not mutated
	if domain.Tables[0].Columns[0].Type != "SERIAL" {
		t.Error("original domain was mutated")
	}

	// Verify indexes were preserved
	if len(mapped.Indexes) != 1 || mapped.Indexes[0].Name != "idx_users_email" {
		t.Error("indexes were not preserved")
	}
}
