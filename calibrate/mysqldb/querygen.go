package mysqldb

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/sam-caldwell/query-test-tool/calibrate"
)

// NewMySQLQueryGenerator creates a query generator with MySQL-compatible templates.
func NewMySQLQueryGenerator(seed int64) *calibrate.QueryGenerator {
	return calibrate.NewQueryGeneratorWithTemplates(seed, mysqlTemplates)
}

// colLiteral returns a type-appropriate literal value for a column.
// MySQL strict mode rejects type mismatches even in EXPLAIN.
func colLiteral(col calibrate.ColumnDef, rng *rand.Rand) string {
	t := col.Type
	switch {
	case strings.Contains(t, "INT") || strings.Contains(t, "DECIMAL") || strings.Contains(t, "NUMERIC"):
		return fmt.Sprintf("%d", rng.Intn(1000)+1)
	case t == "DATE":
		return "'2025-01-15'"
	case t == "DATETIME" || t == "TIMESTAMP":
		return "'2025-01-15 10:30:00'"
	case t == "TINYINT(1)":
		return "1"
	case t == "JSON":
		return "'{\"key\": 1}'"
	default:
		return fmt.Sprintf("'value_%d'", rng.Intn(1000))
	}
}

// textCol returns the first text/varchar column from a list, or the first column.
func textCol(cols []calibrate.ColumnDef) calibrate.ColumnDef {
	for _, c := range cols {
		if c.Type == "TEXT" || strings.HasPrefix(c.Type, "VARCHAR") {
			return c
		}
	}
	return cols[0]
}

func mysqlTemplates(d calibrate.Domain) []calibrate.QueryTempl {
	tables := d.Tables
	if len(tables) == 0 {
		return nil
	}

	var tmpls []calibrate.QueryTempl

	for _, table := range tables {
		t := table // capture
		colDefs := nonSerialColDefs(t)
		cols := nonSerialCols(t)
		if len(cols) == 0 || len(colDefs) == 0 {
			continue
		}

		firstCol := colDefs[0]

		// --- SELECT * (anti) vs SELECT specific (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("select_star", []string{"select-star"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s = %s", t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("select_specific", nil, func(rng *rand.Rand) string {
			cd := colDefs[rng.Intn(len(colDefs))]
			return fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s", cd.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))

		// --- Non-sargable (anti) vs sargable (control) — only on text columns ---
		tc := textCol(colDefs)
		tmpls = append(tmpls, calibrate.NewQueryTempl("non_sargable", []string{"non-sargable"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE LOWER(%s) = 'test'", t.Name, tc.Name)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("sargable", nil, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s = %s", t.Name, tc.Name, colLiteral(tc, rng))
		}))

		// --- Missing predicate (anti) vs with predicate (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("missing_predicate", []string{"missing-predicate"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s", t.Name)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("with_predicate", nil, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s IS NOT NULL", t.Name, firstCol.Name)
		}))

		// --- Unbounded sort (anti) vs bounded (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("unbounded_sort", []string{"unbounded-sort"}, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT * FROM %s ORDER BY %s", t.Name, c)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("bounded_sort", nil, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT 100", t.Name, c)
		}))

		// --- DISTINCT (anti) vs no distinct (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("distinct", []string{"distinct-dedup"}, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT DISTINCT %s FROM %s", c, t.Name)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("no_distinct", nil, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT %s FROM %s", c, t.Name)
		}))

		// --- GROUP BY (anti) vs no group (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("group_by", []string{"group-by-fanout"}, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT %s, COUNT(*) FROM %s GROUP BY %s", c, t.Name, c)
		}))

		// --- LIKE leading wildcard (anti) vs trailing (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("like_wildcard", []string{"like-leading-wildcard"}, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT * FROM %s WHERE %s LIKE '%%test'", t.Name, c)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("like_trailing", nil, func(rng *rand.Rand) string {
			c := cols[rng.Intn(len(cols))]
			return fmt.Sprintf("SELECT * FROM %s WHERE %s LIKE 'test%%'", t.Name, c)
		}))

		// --- Large OFFSET (anti) vs small (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("large_offset", []string{"large-offset"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT 10 OFFSET %d", t.Name, cols[0], 5000+rng.Intn(5000))
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("small_offset", nil, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT 10 OFFSET %d", t.Name, cols[0], rng.Intn(100))
		}))

		// --- Boolean nesting (anti) vs simple (control) ---
		if len(colDefs) >= 2 {
			c0, c1 := colDefs[0], colDefs[1]
			tmpls = append(tmpls, calibrate.NewQueryTempl("boolean_nesting", []string{"boolean-nesting"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE (%s = %s AND %s = %s) OR (%s = %s AND %s = %s)",
					t.Name, c0.Name, colLiteral(c0, rng), c1.Name, colLiteral(c1, rng),
					c0.Name, colLiteral(c0, rng), c1.Name, colLiteral(c1, rng))
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("simple_where", nil, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE %s = %s", t.Name, c0.Name, colLiteral(c0, rng))
			}))
		}

		// --- Subquery (anti) vs join (control) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("subquery", []string{"subquery-nesting"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s IN (SELECT %s FROM %s WHERE %s IS NOT NULL)",
				t.Name, firstCol.Name, firstCol.Name, t.Name, firstCol.Name)
		}))

		// --- CASE expression ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("case_expr", []string{"case-expression"}, func(rng *rand.Rand) string {
			tc := textCol(colDefs)
			return fmt.Sprintf("SELECT CASE WHEN %s IS NULL THEN 'unknown' ELSE %s END AS val FROM %s WHERE %s = %s",
				tc.Name, tc.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))

		// --- UPDATE without WHERE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("update_no_where", []string{"missing-where-clause"}, func(rng *rand.Rand) string {
			tc := textCol(colDefs)
			return fmt.Sprintf("UPDATE %s SET %s = 'updated'", t.Name, tc.Name)
		}))

		// --- DELETE without WHERE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("delete_no_where", []string{"missing-where-clause"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("DELETE FROM %s", t.Name)
		}))
	}

	// --- JOIN patterns (need at least 2 tables) ---
	if len(tables) >= 2 {
		for _, fk := range d.ForeignKeys {
			fkCopy := fk
			tmpls = append(tmpls, calibrate.NewQueryTempl("join", []string{"join"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT a.*, b.* FROM %s a JOIN %s b ON a.%s = b.%s WHERE a.%s = %d",
					fkCopy.Table, fkCopy.RefTable, fkCopy.Column, fkCopy.RefColumn,
					fkCopy.Column, rng.Intn(1000)+1)
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("left_join", []string{"outer-join"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT a.*, b.* FROM %s a LEFT JOIN %s b ON a.%s = b.%s",
					fkCopy.Table, fkCopy.RefTable, fkCopy.Column, fkCopy.RefColumn)
			}))
		}

		// Multi-join
		if len(d.ForeignKeys) >= 2 {
			fk1 := d.ForeignKeys[0]
			fk2 := d.ForeignKeys[1]
			tmpls = append(tmpls, calibrate.NewQueryTempl("multi_join", []string{"join"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT a.*, b.*, c.* FROM %s a JOIN %s b ON a.%s = b.%s JOIN %s c ON a.%s = c.%s",
					fk1.Table, fk1.RefTable, fk1.Column, fk1.RefColumn,
					fk2.RefTable, fk2.Column, fk2.RefColumn)
			}))
		}
	}

	// --- UNION ---
	if len(tables) >= 2 {
		t1, t2 := tables[0], tables[1]
		c1 := nonSerialCols(t1)
		c2 := nonSerialCols(t2)
		if len(c1) > 0 && len(c2) > 0 {
			tmpls = append(tmpls, calibrate.NewQueryTempl("union", []string{"set-operation"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT %s FROM %s UNION SELECT %s FROM %s",
					c1[0], t1.Name, c2[0], t2.Name)
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("union_all", nil, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT %s FROM %s UNION ALL SELECT %s FROM %s",
					c1[0], t1.Name, c2[0], t2.Name)
			}))
		}
	}

	return tmpls
}

func nonSerialCols(t calibrate.TableDef) []string {
	var cols []string
	for _, c := range t.Columns {
		if !c.IsSerial && !IsAutoIncrement(c.Type) {
			cols = append(cols, c.Name)
		}
	}
	return cols
}

func nonSerialColDefs(t calibrate.TableDef) []calibrate.ColumnDef {
	var cols []calibrate.ColumnDef
	for _, c := range t.Columns {
		if !c.IsSerial && !IsAutoIncrement(c.Type) {
			cols = append(cols, c)
		}
	}
	return cols
}
