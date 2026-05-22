package mysqldb

import (
	"fmt"
	"strings"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

// MySQLDDLGenerator implements calibrate.DDLGenerator for MySQL.
type MySQLDDLGenerator struct{}

// MySQLDDLGenerator uses table name prefixes within a single database
// (e.g., cal_00001_users) since MySQL users typically don't have CREATE DATABASE privilege.
// The "schemaName" parameter becomes a table prefix.

func (g *MySQLDDLGenerator) GenerateDDL(d calibrate.Domain, schemaName string) string {
	var b strings.Builder
	for _, table := range d.Tables {
		b.WriteString(generateCreateTable(schemaName, table))
		b.WriteString("\n")
	}
	for _, idx := range d.Indexes {
		b.WriteString(generateCreateIndex(schemaName, idx))
		b.WriteString("\n")
	}
	for _, fk := range d.ForeignKeys {
		b.WriteString(generateAddFK(schemaName, fk))
		b.WriteString("\n")
	}
	return b.String()
}

func (g *MySQLDDLGenerator) GenerateDDLTablesOnly(d calibrate.Domain, schemaName string) string {
	var b strings.Builder
	for _, table := range d.Tables {
		b.WriteString(generateCreateTable(schemaName, table))
		b.WriteString("\n")
	}
	return b.String()
}

func (g *MySQLDDLGenerator) GenerateDDLIndexesAndFKs(d calibrate.Domain, schemaName string) string {
	var b strings.Builder
	for _, idx := range d.Indexes {
		b.WriteString(generateCreateIndex(schemaName, idx))
		b.WriteString("\n")
	}
	for _, fk := range d.ForeignKeys {
		b.WriteString(generateAddFK(schemaName, fk))
		b.WriteString("\n")
	}
	return b.String()
}

// prefixedName returns the table name with the schema prefix: cal_00001_users
func prefixedName(schema, name string) string {
	return fmt.Sprintf("%s_%s", schema, name)
}

func generateCreateTable(schema string, t calibrate.TableDef) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (\n", prefixedName(schema, t.Name)))

	var pkCol string
	for i, col := range t.Columns {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(fmt.Sprintf("  `%s` %s", col.Name, col.Type))

		if IsAutoIncrement(col.Type) {
			pkCol = col.Name
		}

		if col.NotNull && !IsAutoIncrement(col.Type) {
			b.WriteString(" NOT NULL")
		}
		if col.Default != "" && !IsAutoIncrement(col.Type) {
			b.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
		}
	}

	if pkCol != "" {
		b.WriteString(fmt.Sprintf(",\n  PRIMARY KEY (`%s`)", pkCol))
	}

	b.WriteString("\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n")
	return b.String()
}

func generateCreateIndex(schema string, idx calibrate.IndexDef) string {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	cols := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		cols[i] = fmt.Sprintf("`%s`", c)
	}
	return fmt.Sprintf("CREATE %sINDEX `%s_%s` ON `%s` (%s);\n",
		unique, schema, idx.Name, prefixedName(schema, idx.Table), strings.Join(cols, ", "))
}

func generateAddFK(schema string, fk calibrate.FKDef) string {
	return fmt.Sprintf("ALTER TABLE `%s` ADD CONSTRAINT `%s_%s` FOREIGN KEY (`%s`) REFERENCES `%s` (`%s`);\n",
		prefixedName(schema, fk.Table), schema, fk.Name, fk.Column, prefixedName(schema, fk.RefTable), fk.RefColumn)
}
