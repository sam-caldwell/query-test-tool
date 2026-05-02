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

// hasCompositeUnique returns true if the table has a multi-column unique index.
func hasCompositeUnique(table TableDef, domain Domain) bool {
	for _, idx := range domain.Indexes {
		if idx.Table == table.Name && idx.Unique && len(idx.Columns) > 1 {
			return true
		}
	}
	return false
}

// generateInsertSQL creates an INSERT ... SELECT generate_series statement.
func (dg *DataGenerator) generateInsertSQL(schema string, table TableDef, domain Domain) string {
	baseRows := dg.cfg.RowsPerTable
	rows := baseRows

	// For join/child tables, use more rows unless constrained by composite unique
	if isChildTable(table, domain) {
		if hasCompositeUnique(table, domain) {
			rows = baseRows // can't exceed unique combinations
		} else {
			rows = baseRows * 3
		}
	}

	var cols []string
	var exprs []string

	for _, col := range table.Columns {
		if col.IsSerial {
			continue // auto-generated
		}
		cols = append(cols, col.Name)
		exprs = append(exprs, dataExpression(col, rows, baseRows, domain, table))
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
// totalRows is the row count for the current table, baseRows is the parent table row count.
func dataExpression(col ColumnDef, totalRows int, baseRows int, domain Domain, table TableDef) string {
	// FK columns: reference parent table IDs (parent tables always have baseRows rows with IDs 1..baseRows)
	for _, fk := range domain.ForeignKeys {
		if fk.Table == table.Name && fk.Column == col.Name {
			return fmt.Sprintf("((i - 1) %% %d) + 1", baseRows)
		}
	}

	// Unique indexed columns must use deterministic values based on i
	if isUniqueColumn(col.Name, table.Name, domain) {
		switch {
		case strings.HasPrefix(col.Type, "VARCHAR") || col.Type == "TEXT":
			return fmt.Sprintf("'%s_' || i", col.Name)
		default:
			return "i"
		}
	}

	expr := baseValueExpression(col, totalRows)

	// Inject sporadic NULLs for nullable columns (~10% NULL rate).
	// This ensures queries encounter real NULL values for realistic cost estimates.
	if !col.NotNull {
		expr = fmt.Sprintf("CASE WHEN random() < 0.10 THEN NULL ELSE %s END", expr)
	}

	return expr
}

// baseValueExpression generates the core value expression for a column type.
// Uses high-cardinality distributions: zipfian for text, varied ranges for numerics.
func baseValueExpression(col ColumnDef, totalRows int) string {
	switch {
	case strings.Contains(col.Type, "SERIAL"):
		return "i"
	case col.Type == "TEXT" || strings.HasPrefix(col.Type, "VARCHAR"):
		return textExpression(col.Name, totalRows)
	case col.Type == "INT" || col.Type == "BIGINT" || col.Type == "SMALLINT":
		// Skewed distribution: mix of low-cardinality hot values and high-cardinality tail
		return fmt.Sprintf("CASE WHEN random() < 0.3 THEN (random() * 10)::int ELSE (random() * %d)::int END", totalRows)
	case strings.HasPrefix(col.Type, "NUMERIC"):
		maxVal := 10000.0
		if len(col.Type) > 8 {
			parts := strings.TrimPrefix(col.Type, "NUMERIC(")
			parts = strings.TrimSuffix(parts, ")")
			fields := strings.Split(parts, ",")
			if len(fields) == 2 {
				var prec, scale int
				fmt.Sscanf(fields[0], "%d", &prec)
				fmt.Sscanf(fields[1], "%d", &scale)
				intDigits := prec - scale
				if intDigits > 0 {
					maxVal = 1.0
					for j := 0; j < intDigits; j++ {
						maxVal *= 10
					}
					maxVal -= 0.01
				}
			}
		}
		// Skewed: some values cluster near 0, others spread across range
		return fmt.Sprintf("(power(random(), 2) * %.2f)::%s", maxVal, col.Type)
	case col.Type == "BOOLEAN":
		// Skewed: 70% true, 30% false (realistic for is_active type columns)
		return "(random() > 0.3)"
	case col.Type == "DATE":
		// Cluster recent dates more heavily (exponential decay)
		return "CURRENT_DATE - (power(random(), 2) * 730)::int"
	case col.Type == "TIMESTAMPTZ" || col.Type == "TIMESTAMP":
		return "now() - (power(random(), 2) * interval '730 days')"
	case col.Type == "JSONB":
		return "jsonb_build_object('key', i, 'value', random(), 'tags', ARRAY[(random()*10)::int, (random()*10)::int])"
	default:
		return "'test_value'"
	}
}

// textExpression generates realistic text data based on column name.
// Uses high-cardinality distributions with realistic patterns.
func textExpression(colName string, totalRows int) string {
	switch {
	case strings.Contains(colName, "email"):
		return "'user_' || i || '@example.com'"
	case strings.Contains(colName, "name") || strings.Contains(colName, "title"):
		// High cardinality: unique-ish values with some repeats for realistic join behavior
		return fmt.Sprintf("'name_' || (power(random(), 0.5) * %d)::int", totalRows)
	case strings.Contains(colName, "status"):
		// Skewed: 60% active, 20% completed, 10% pending, 10% cancelled (realistic)
		return "CASE WHEN random() < 0.6 THEN 'active' WHEN random() < 0.8 THEN 'completed' WHEN random() < 0.9 THEN 'pending' ELSE 'cancelled' END"
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

// isUniqueColumn returns true if the column has a unique index.
func isUniqueColumn(colName, tableName string, domain Domain) bool {
	for _, idx := range domain.Indexes {
		if idx.Table == tableName && idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == colName {
			return true
		}
	}
	return false
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
