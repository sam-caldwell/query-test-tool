package calibrate

import (
	"context"
	"fmt"
	"strings"
)

// DataGenerator populates schemas with test data.
type DataGenerator struct {
	db  *DB
	cfg PipelineConfig
}

// NewDataGenerator creates a new data generator.
func NewDataGenerator(db *DB, cfg PipelineConfig) *DataGenerator {
	return &DataGenerator{db: db, cfg: cfg}
}

// PopulateSchema generates test data for all tables in a schema.
// It uses generate_series and random() for efficient bulk insertion.
func (dg *DataGenerator) PopulateSchema(ctx context.Context, schemaName string, domain Domain) error {
	// Insert data respecting FK ordering (parent tables first)
	ordered := topologicalSort(domain)

	for _, table := range ordered {
		sql := dg.generateInsertSQL(schemaName, table, domain)
		if _, err := dg.db.conn.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("populating %s.%s: %w", schemaName, table.Name, err)
		}
	}

	// ANALYZE to update statistics
	for _, table := range domain.Tables {
		if _, err := dg.db.conn.ExecContext(ctx, fmt.Sprintf("ANALYZE %s.%s", schemaName, table.Name)); err != nil {
			return fmt.Errorf("analyzing %s.%s: %w", schemaName, table.Name, err)
		}
	}

	return nil
}

// generateInsertSQL creates an INSERT ... SELECT generate_series statement.
func (dg *DataGenerator) generateInsertSQL(schema string, table TableDef, domain Domain) string {
	rows := dg.cfg.RowsPerTable

	// For join/child tables, use fewer rows proportionally
	if isChildTable(table, domain) {
		rows = rows * 3 // 3x rows for child tables (realistic ratio)
	}

	var cols []string
	var exprs []string

	for _, col := range table.Columns {
		if col.IsSerial {
			continue // auto-generated
		}
		cols = append(cols, col.Name)
		exprs = append(exprs, dataExpression(col, rows, domain, table))
	}

	return fmt.Sprintf(
		"INSERT INTO %s.%s (%s)\nSELECT %s\nFROM generate_series(1, %d) AS i;\n",
		schema, table.Name,
		strings.Join(cols, ", "),
		strings.Join(exprs, ",\n       "),
		rows,
	)
}

// dataExpression generates a SQL expression to produce realistic test data for a column.
func dataExpression(col ColumnDef, totalRows int, domain Domain, table TableDef) string {
	// FK columns: reference parent table IDs
	for _, fk := range domain.ForeignKeys {
		if fk.Table == table.Name && fk.Column == col.Name {
			// Reference a random existing parent row
			return fmt.Sprintf("(1 + (random() * %d)::int %% %d + 1)", totalRows, totalRows)
		}
	}

	switch {
	case strings.Contains(col.Type, "SERIAL"):
		return "i"
	case col.Type == "TEXT" || strings.HasPrefix(col.Type, "VARCHAR"):
		return textExpression(col.Name, totalRows)
	case col.Type == "INT" || col.Type == "BIGINT" || col.Type == "SMALLINT":
		return fmt.Sprintf("(random() * %d)::int", totalRows)
	case strings.HasPrefix(col.Type, "NUMERIC"):
		return "(random() * 10000)::numeric(10,2)"
	case col.Type == "BOOLEAN":
		return "(random() > 0.5)"
	case col.Type == "DATE":
		return "CURRENT_DATE - (random() * 365)::int"
	case col.Type == "TIMESTAMPTZ" || col.Type == "TIMESTAMP":
		return "now() - (random() * interval '365 days')"
	case col.Type == "JSONB":
		return "jsonb_build_object('key', i, 'value', random())"
	default:
		return "'test_value'"
	}
}

// textExpression generates realistic text data based on column name.
func textExpression(colName string, totalRows int) string {
	switch {
	case strings.Contains(colName, "email"):
		return "'user_' || i || '@example.com'"
	case strings.Contains(colName, "name") || strings.Contains(colName, "title"):
		return fmt.Sprintf("'name_' || (random() * %d)::int", totalRows)
	case strings.Contains(colName, "status"):
		return "(ARRAY['active','pending','completed','cancelled'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "url"):
		return "'https://example.com/page/' || i"
	case strings.Contains(colName, "country"):
		return "(ARRAY['US','UK','DE','FR','JP','CA','AU','BR','IN','CN'])[1 + (random() * 9)::int]"
	case strings.Contains(colName, "type"):
		return "(ARRAY['type_a','type_b','type_c','type_d'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "slug"):
		return "'slug-' || i"
	case strings.Contains(colName, "sku"):
		return "'SKU-' || lpad(i::text, 6, '0')"
	case strings.Contains(colName, "role"):
		return "(ARRAY['engineer','manager','designer','analyst'])[1 + (random() * 3)::int]"
	case strings.Contains(colName, "source"):
		return "(ARRAY['google','direct','email','social','referral'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "bio") || strings.Contains(colName, "body"):
		return "'Lorem ipsum dolor sit amet ' || i"
	case strings.Contains(colName, "location"):
		return "(ARRAY['New York','London','Tokyo','Berlin','Sydney'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "device"):
		return "(ARRAY['mobile','desktop','tablet'])[1 + (random() * 2)::int]"
	case strings.Contains(colName, "section"):
		return "(ARRAY['home','blog','docs','pricing','about'])[1 + (random() * 4)::int]"
	case strings.Contains(colName, "external_id"):
		return "'ext_' || lpad(i::text, 8, '0')"
	default:
		return "'value_' || i"
	}
}

// isChildTable returns true if the table has a FK referencing another table.
func isChildTable(table TableDef, domain Domain) bool {
	for _, fk := range domain.ForeignKeys {
		if fk.Table == table.Name {
			return true
		}
	}
	return false
}

// topologicalSort orders tables so parents come before children.
func topologicalSort(domain Domain) []TableDef {
	// Build dependency graph
	deps := make(map[string][]string) // table -> tables it depends on
	tableMap := make(map[string]TableDef)
	for _, t := range domain.Tables {
		tableMap[t.Name] = t
		deps[t.Name] = nil
	}
	for _, fk := range domain.ForeignKeys {
		if fk.Table != fk.RefTable { // skip self-references
			deps[fk.Table] = append(deps[fk.Table], fk.RefTable)
		}
	}

	// Kahn's algorithm
	var sorted []TableDef
	visited := make(map[string]bool)
	var visit func(name string)
	visiting := make(map[string]bool)

	visit = func(name string) {
		if visited[name] {
			return
		}
		if visiting[name] {
			// Cycle — just add it
			visited[name] = true
			sorted = append(sorted, tableMap[name])
			return
		}
		visiting[name] = true
		for _, dep := range deps[name] {
			visit(dep)
		}
		visiting[name] = false
		visited[name] = true
		sorted = append(sorted, tableMap[name])
	}

	for _, t := range domain.Tables {
		visit(t.Name)
	}
	return sorted
}
