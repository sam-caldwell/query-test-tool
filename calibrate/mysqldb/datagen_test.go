package mysqldb

import (
	"strings"
	"testing"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

func TestGenerateInsertSQL_UsesRecursiveCTE(t *testing.T) {
	domain := MapPgTypesToMySQL(calibrate.Archetypes()[0])
	dp := &MySQLDataPopulator{cfg: calibrate.PipelineConfig{RowsPerTable: 100}}

	sql := dp.generateInsertSQL("cal_00001", domain.Tables[0], domain)
	if !strings.Contains(sql, "WITH RECURSIVE seq") {
		t.Error("MySQL INSERT should use recursive CTE instead of generate_series")
	}
	if !strings.Contains(sql, "INSERT INTO `cal_00001_") {
		t.Error("should reference prefixed table name")
	}
}

func TestGenerateInsertSQL_NoPostgreSQLSyntax(t *testing.T) {
	domain := MapPgTypesToMySQL(calibrate.Archetypes()[0])
	dp := &MySQLDataPopulator{cfg: calibrate.PipelineConfig{RowsPerTable: 100}}

	for _, table := range domain.Tables {
		sql := dp.generateInsertSQL("cal_00001", table, domain)
		pgPatterns := []string{"generate_series", "random()", "::text", "::int", "now()", "CURRENT_DATE -"}
		for _, p := range pgPatterns {
			if strings.Contains(sql, p) {
				t.Errorf("table %s: MySQL INSERT contains PostgreSQL syntax %q", table.Name, p)
			}
		}
	}
}

func TestGenerateInsertSQL_UsesMySQLFunctions(t *testing.T) {
	domain := MapPgTypesToMySQL(calibrate.Archetypes()[0])
	dp := &MySQLDataPopulator{cfg: calibrate.PipelineConfig{RowsPerTable: 100}}

	// Collect all SQL across all tables
	var allSQL strings.Builder
	for _, table := range domain.Tables {
		allSQL.WriteString(dp.generateInsertSQL("cal_00001", table, domain))
	}
	sql := allSQL.String()

	// Should use MySQL functions
	mysqlFuncs := []string{"RAND()", "CONCAT(", "CURDATE()", "NOW()"}
	found := 0
	for _, f := range mysqlFuncs {
		if strings.Contains(sql, f) {
			found++
		}
	}
	if found < 2 {
		t.Errorf("expected at least 2 MySQL-specific functions, found %d", found)
	}
}

func TestMysqlDataExpression_FKColumn(t *testing.T) {
	domain := calibrate.Domain{
		Tables: []calibrate.TableDef{
			{Name: "orders", Columns: []calibrate.ColumnDef{
				{Name: "user_id", Type: "INT", NotNull: true},
			}},
		},
		ForeignKeys: []calibrate.FKDef{
			{Table: "orders", Column: "user_id", RefTable: "users", RefColumn: "id"},
		},
	}

	expr := mysqlDataExpression(
		calibrate.ColumnDef{Name: "user_id", Type: "INT", NotNull: true},
		300, 100, domain, domain.Tables[0],
	)
	if !strings.Contains(expr, "% 100") {
		t.Errorf("FK expression should reference baseRows (100), got: %s", expr)
	}
}

func TestMysqlDataExpression_NullInjection(t *testing.T) {
	domain := calibrate.Domain{
		Tables: []calibrate.TableDef{
			{Name: "test", Columns: []calibrate.ColumnDef{
				{Name: "notes", Type: "TEXT", NotNull: false},
			}},
		},
	}

	expr := mysqlDataExpression(
		calibrate.ColumnDef{Name: "notes", Type: "TEXT", NotNull: false},
		100, 100, domain, domain.Tables[0],
	)
	if !strings.Contains(expr, "IF(RAND() < 0.10, NULL") {
		t.Errorf("nullable column should have NULL injection, got: %s", expr)
	}
}

func TestMysqlDataExpression_NotNullNoInjection(t *testing.T) {
	domain := calibrate.Domain{
		Tables: []calibrate.TableDef{
			{Name: "test", Columns: []calibrate.ColumnDef{
				{Name: "name", Type: "VARCHAR(100)", NotNull: true},
			}},
		},
	}

	expr := mysqlDataExpression(
		calibrate.ColumnDef{Name: "name", Type: "VARCHAR(100)", NotNull: true},
		100, 100, domain, domain.Tables[0],
	)
	if strings.Contains(expr, "NULL") {
		t.Errorf("NOT NULL column should not have NULL injection, got: %s", expr)
	}
}

func TestMysqlTextExpression(t *testing.T) {
	tests := []struct {
		colName  string
		contains string
	}{
		{"email", "@example.com"},
		{"name", "CONCAT('name_'"},
		{"status", "ELT("},
		{"url", "https://"},
		{"description", "Lorem"},
		{"unknown_col", "CONCAT('value_'"},
	}

	for _, tt := range tests {
		expr := mysqlTextExpression(tt.colName, 1000)
		if !strings.Contains(expr, tt.contains) {
			t.Errorf("mysqlTextExpression(%q) = %q, expected to contain %q", tt.colName, expr, tt.contains)
		}
	}
}

func TestMysqlBaseValueExpression_AllTypes(t *testing.T) {
	tests := []struct {
		col      calibrate.ColumnDef
		contains string
	}{
		{calibrate.ColumnDef{Name: "id", Type: "INT AUTO_INCREMENT"}, "i"},
		{calibrate.ColumnDef{Name: "name", Type: "VARCHAR(100)"}, "CONCAT"},
		{calibrate.ColumnDef{Name: "age", Type: "INT"}, "RAND()"},
		{calibrate.ColumnDef{Name: "price", Type: "DECIMAL(10,2)"}, "POW(RAND()"},
		{calibrate.ColumnDef{Name: "active", Type: "TINYINT(1)"}, "RAND()"},
		{calibrate.ColumnDef{Name: "born", Type: "DATE"}, "CURDATE()"},
		{calibrate.ColumnDef{Name: "created", Type: "DATETIME"}, "NOW()"},
		{calibrate.ColumnDef{Name: "meta", Type: "JSON"}, "JSON_OBJECT"},
		{calibrate.ColumnDef{Name: "other", Type: "BLOB"}, "test_value"},
	}

	for _, tt := range tests {
		expr := mysqlBaseValueExpression(tt.col, 1000)
		if !strings.Contains(expr, tt.contains) {
			t.Errorf("mysqlBaseValueExpression(%s, %s) = %q, expected to contain %q",
				tt.col.Name, tt.col.Type, expr, tt.contains)
		}
	}
}
