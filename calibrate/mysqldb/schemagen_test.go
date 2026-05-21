package mysqldb

import (
	"strings"
	"testing"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

func testDomain() calibrate.Domain {
	return MapPgTypesToMySQL(calibrate.Archetypes()[0])
}

func TestGenerateDDL_ContainsCreateDatabase(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "CREATE DATABASE IF NOT EXISTS `cal_00001`") {
		t.Error("DDL should contain CREATE DATABASE")
	}
}

func TestGenerateDDL_ContainsCreateTable(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "CREATE TABLE `cal_00001`.") {
		t.Error("DDL should contain CREATE TABLE with database prefix")
	}
}

func TestGenerateDDL_ContainsEngine(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "ENGINE=InnoDB") {
		t.Error("DDL should specify InnoDB engine")
	}
}

func TestGenerateDDL_ContainsAutoIncrement(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "AUTO_INCREMENT") {
		t.Error("DDL should contain AUTO_INCREMENT for PK columns")
	}
}

func TestGenerateDDL_ContainsPrimaryKey(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "PRIMARY KEY") {
		t.Error("DDL should contain PRIMARY KEY")
	}
}

func TestGenerateDDL_ContainsIndexes(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "CREATE INDEX") || !strings.Contains(ddl, "CREATE UNIQUE INDEX") {
		t.Error("DDL should contain CREATE INDEX and CREATE UNIQUE INDEX")
	}
}

func TestGenerateDDL_ContainsForeignKeys(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	if !strings.Contains(ddl, "FOREIGN KEY") {
		t.Error("DDL should contain FOREIGN KEY")
	}
}

func TestGenerateDDLTablesOnly_NoIndexes(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDLTablesOnly(testDomain(), "cal_00001")
	if strings.Contains(ddl, "CREATE INDEX") {
		t.Error("TablesOnly DDL should not contain CREATE INDEX")
	}
	if strings.Contains(ddl, "FOREIGN KEY") {
		t.Error("TablesOnly DDL should not contain FOREIGN KEY")
	}
	if !strings.Contains(ddl, "CREATE TABLE") {
		t.Error("TablesOnly DDL should contain CREATE TABLE")
	}
}

func TestGenerateDDLIndexesAndFKs_NoTables(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDLIndexesAndFKs(testDomain(), "cal_00001")
	if strings.Contains(ddl, "CREATE TABLE") {
		t.Error("IndexesAndFKs DDL should not contain CREATE TABLE")
	}
	if strings.Contains(ddl, "CREATE DATABASE") {
		t.Error("IndexesAndFKs DDL should not contain CREATE DATABASE")
	}
}

func TestGenerateDDL_NoPostgreSQLSyntax(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	pgPatterns := []string{"SERIAL PRIMARY KEY", "BIGSERIAL", "TIMESTAMPTZ", "JSONB", "UNLOGGED", "CREATE SCHEMA"}
	for _, p := range pgPatterns {
		if strings.Contains(ddl, p) {
			t.Errorf("MySQL DDL should not contain PostgreSQL syntax %q", p)
		}
	}
}

func TestGenerateDDL_BacktickQuoting(t *testing.T) {
	g := &MySQLDDLGenerator{}
	ddl := g.GenerateDDL(testDomain(), "cal_00001")
	// MySQL uses backticks, not double quotes
	if !strings.Contains(ddl, "`cal_00001`") {
		t.Error("DDL should use backtick quoting for identifiers")
	}
}

func TestGenerateCreateTable_Defaults(t *testing.T) {
	table := calibrate.TableDef{
		Name: "test",
		Columns: []calibrate.ColumnDef{
			{Name: "id", Type: "INT AUTO_INCREMENT"},
			{Name: "name", Type: "VARCHAR(100)", NotNull: true},
			{Name: "balance", Type: "DECIMAL(10,2)", NotNull: true, Default: "0"},
		},
	}
	ddl := generateCreateTable("db1", table)
	if !strings.Contains(ddl, "DEFAULT 0") {
		t.Error("should contain DEFAULT for balance column")
	}
	if !strings.Contains(ddl, "NOT NULL") {
		t.Error("should contain NOT NULL for name column")
	}
}
