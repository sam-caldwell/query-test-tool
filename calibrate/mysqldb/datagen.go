package mysqldb

import (
	"context"
	"fmt"
	"strings"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

// MySQLDataPopulator populates schemas with test data using MySQL syntax.
type MySQLDataPopulator struct {
	db  calibrate.DialectDB
	cfg calibrate.PipelineConfig
}

// NewMySQLDataPopulator creates a new MySQL data populator.
func NewMySQLDataPopulator(db calibrate.DialectDB, cfg calibrate.PipelineConfig) *MySQLDataPopulator {
	return &MySQLDataPopulator{db: db, cfg: cfg}
}

// PopulateSchema generates test data for all tables in a MySQL database/schema.
func (dp *MySQLDataPopulator) PopulateSchema(ctx context.Context, schemaName string, domain calibrate.Domain) error {
	// Optimize for bulk inserts
	if _, err := dp.db.Conn().ExecContext(ctx, "SET foreign_key_checks = 0"); err != nil {
		return fmt.Errorf("disabling FK checks: %w", err)
	}
	defer dp.db.Conn().ExecContext(ctx, "SET foreign_key_checks = 1") //nolint

	// Insert data respecting FK ordering
	ordered := calibrate.TopologicalSort(domain)

	for _, table := range ordered {
		sql := dp.generateInsertSQL(schemaName, table, domain)
		if _, err := dp.db.Conn().ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("populating %s.%s: %w", schemaName, table.Name, err)
		}
	}

	// ANALYZE tables for optimizer statistics
	for _, table := range domain.Tables {
		if _, err := dp.db.Conn().ExecContext(ctx, fmt.Sprintf("ANALYZE TABLE `%s`.`%s`", schemaName, table.Name)); err != nil {
			return fmt.Errorf("analyzing %s.%s: %w", schemaName, table.Name, err)
		}
	}

	return nil
}

func (dp *MySQLDataPopulator) generateInsertSQL(schema string, table calibrate.TableDef, domain calibrate.Domain) string {
	baseRows := dp.cfg.RowsPerTable
	multiplier := calibrate.TableRowMultiplier(table, domain)
	rows := baseRows * multiplier

	var cols []string
	var exprs []string

	for _, col := range table.Columns {
		if IsAutoIncrement(col.Type) {
			continue
		}
		cols = append(cols, fmt.Sprintf("`%s`", col.Name))
		exprs = append(exprs, mysqlDataExpression(col, rows, baseRows, domain, table))
	}

	// MySQL doesn't have generate_series. Use a recursive CTE.
	// For large row counts, we use a cross-join approach for speed.
	return fmt.Sprintf(
		"INSERT INTO `%s`.`%s` (%s)\n"+
			"WITH RECURSIVE seq AS (\n"+
			"  SELECT 1 AS i\n"+
			"  UNION ALL\n"+
			"  SELECT i + 1 FROM seq WHERE i < %d\n"+
			")\n"+
			"SELECT %s\nFROM seq;\n",
		schema, table.Name,
		strings.Join(cols, ", "),
		rows,
		strings.Join(exprs, ",\n       "),
	)
}

func mysqlDataExpression(col calibrate.ColumnDef, totalRows int, baseRows int, domain calibrate.Domain, table calibrate.TableDef) string {
	// FK columns
	for _, fk := range domain.ForeignKeys {
		if fk.Table == table.Name && fk.Column == col.Name {
			return fmt.Sprintf("((i - 1) %% %d) + 1", baseRows)
		}
	}

	// Unique indexed columns
	if calibrate.IsUniqueColumn(col.Name, table.Name, domain) {
		switch {
		case strings.HasPrefix(col.Type, "VARCHAR") || col.Type == "TEXT":
			return fmt.Sprintf("CONCAT('%s_', i)", col.Name)
		default:
			return "i"
		}
	}

	expr := mysqlBaseValueExpression(col, totalRows)

	// Inject sporadic NULLs for nullable columns
	if !col.NotNull {
		expr = fmt.Sprintf("IF(RAND() < 0.10, NULL, %s)", expr)
	}

	return expr
}

func mysqlBaseValueExpression(col calibrate.ColumnDef, totalRows int) string {
	mysqlType := col.Type

	switch {
	case IsAutoIncrement(mysqlType):
		return "i"
	case mysqlType == "TEXT" || strings.HasPrefix(mysqlType, "VARCHAR"):
		return mysqlTextExpression(col.Name, totalRows)
	case mysqlType == "INT" || mysqlType == "BIGINT" || mysqlType == "SMALLINT":
		return fmt.Sprintf("IF(RAND() < 0.3, FLOOR(RAND() * 10), FLOOR(RAND() * %d))", totalRows)
	case strings.HasPrefix(mysqlType, "DECIMAL"):
		maxVal := 10000.0
		if len(mysqlType) > 8 {
			parts := strings.TrimPrefix(mysqlType, "DECIMAL(")
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
		return fmt.Sprintf("CAST(POW(RAND(), 2) * %.2f AS DECIMAL(10,2))", maxVal)
	case mysqlType == "TINYINT(1)":
		return "IF(RAND() > 0.3, 1, 0)"
	case mysqlType == "DATE":
		return "DATE_SUB(CURDATE(), INTERVAL FLOOR(POW(RAND(), 2) * 730) DAY)"
	case mysqlType == "DATETIME":
		return "DATE_SUB(NOW(), INTERVAL FLOOR(POW(RAND(), 2) * 730 * 24 * 60) MINUTE)"
	case mysqlType == "JSON":
		return "JSON_OBJECT('key', i, 'value', RAND())"
	default:
		return "'test_value'"
	}
}

func mysqlTextExpression(colName string, totalRows int) string {
	switch {
	case strings.Contains(colName, "email"):
		return "CONCAT('user_', i, '@example.com')"
	case strings.Contains(colName, "name") || strings.Contains(colName, "title"):
		return fmt.Sprintf("CONCAT('name_', FLOOR(POW(RAND(), 0.5) * %d))", totalRows)
	case strings.Contains(colName, "status"):
		return "ELT(1 + FLOOR(RAND() * 4), 'active', 'active', 'active', 'completed')"
	case strings.Contains(colName, "url"):
		return "CONCAT('https://example.com/', i)"
	case strings.Contains(colName, "description"), strings.Contains(colName, "bio"),
		strings.Contains(colName, "body"), strings.Contains(colName, "notes"):
		return "CONCAT('Lorem ipsum dolor sit amet, item ', i)"
	default:
		return fmt.Sprintf("CONCAT('value_', FLOOR(RAND() * %d))", totalRows)
	}
}
