package mysqldb

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/sam-caldwell/query-test-tool/src/calibrate"
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

	// =========================================================================
	// MySQL-Specific Features
	// =========================================================================

	for _, table := range tables {
		t := table
		colDefs := nonSerialColDefs(t)
		cols := nonSerialCols(t)
		if len(cols) == 0 || len(colDefs) == 0 {
			continue
		}
		firstCol := colDefs[0]
		tc := textCol(colDefs)

		// --- RIGHT JOIN ---
		for _, fk := range d.ForeignKeys {
			if fk.Table == t.Name {
				fkCopy := fk
				tmpls = append(tmpls, calibrate.NewQueryTempl("right_join", []string{"outer-join"}, func(rng *rand.Rand) string {
					return fmt.Sprintf("SELECT a.*, b.* FROM %s a RIGHT JOIN %s b ON a.%s = b.%s",
						fkCopy.RefTable, fkCopy.Table, fkCopy.RefColumn, fkCopy.Column)
				}))
				break
			}
		}

		// --- Correlated subquery ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("correlated_subquery", []string{"correlated-subquery"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s a WHERE EXISTS (SELECT 1 FROM %s b WHERE b.%s = a.%s)",
				t.Name, t.Name, firstCol.Name, firstCol.Name)
		}))

		// --- Derived table ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("derived_table", []string{"subquery-nesting"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT dt.cnt FROM (SELECT %s, COUNT(*) AS cnt FROM %s GROUP BY %s) AS dt WHERE dt.cnt > 1",
				firstCol.Name, t.Name, firstCol.Name)
		}))

		// --- HAVING clause ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("having", []string{"group-by-fanout"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, COUNT(*) AS cnt FROM %s GROUP BY %s HAVING cnt > %d",
				firstCol.Name, t.Name, firstCol.Name, rng.Intn(5)+1)
		}))

		// --- GROUP BY ... WITH ROLLUP ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("rollup", []string{"group-by-fanout"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, COUNT(*) FROM %s GROUP BY %s WITH ROLLUP",
				firstCol.Name, t.Name, firstCol.Name)
		}))

		// --- COALESCE / IFNULL ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("coalesce_predicate", []string{"null-coalesce-in-predicate"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE COALESCE(%s, %s) = %s",
				t.Name, tc.Name, colLiteral(tc, rng), colLiteral(tc, rng))
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("ifnull", []string{"null-coalesce-in-predicate"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE IFNULL(%s, %s) = %s",
				t.Name, tc.Name, colLiteral(tc, rng), colLiteral(tc, rng))
		}))

		// --- NULL check chains ---
		if len(colDefs) >= 3 {
			c0, c1, c2 := colDefs[0], colDefs[1], colDefs[2]
			tmpls = append(tmpls, calibrate.NewQueryTempl("null_chain", []string{"null-check-chain"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE %s IS NULL AND %s IS NULL AND %s IS NOT NULL",
					t.Name, c0.Name, c1.Name, c2.Name)
			}))
		}

		// --- REGEXP / RLIKE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("regexp", []string{"non-sargable"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s REGEXP '^[A-Z]'", t.Name, tc.Name)
		}))

		// --- BETWEEN ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("between", nil, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s BETWEEN %s AND %s",
				t.Name, firstCol.Name, colLiteral(firstCol, rng), colLiteral(firstCol, rng))
		}))

		// --- Large IN list ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("large_in_list", []string{"large-in-list"}, func(rng *rand.Rand) string {
			vals := make([]string, 50)
			for i := range vals {
				vals[i] = colLiteral(firstCol, rng)
			}
			return fmt.Sprintf("SELECT * FROM %s WHERE %s IN (%s)",
				t.Name, firstCol.Name, strings.Join(vals, ", "))
		}))

		// --- FOR UPDATE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("for_update", []string{"for-update-lock"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s = %s FOR UPDATE",
				t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))

		// --- LOCK IN SHARE MODE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("lock_share", []string{"for-update-lock"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE %s = %s LOCK IN SHARE MODE",
				t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))

		// --- Window functions ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("window_func", []string{"window-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, ROW_NUMBER() OVER (ORDER BY %s) AS rn FROM %s",
				firstCol.Name, firstCol.Name, t.Name)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("window_no_partition", []string{"window-no-partition-extra"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, RANK() OVER (ORDER BY %s) AS rnk FROM %s",
				firstCol.Name, firstCol.Name, t.Name)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("window_partitioned", []string{"window-function"}, func(rng *rand.Rand) string {
			if len(colDefs) >= 2 {
				return fmt.Sprintf("SELECT %s, %s, ROW_NUMBER() OVER (PARTITION BY %s ORDER BY %s) AS rn FROM %s",
					colDefs[0].Name, colDefs[1].Name, colDefs[0].Name, colDefs[1].Name, t.Name)
			}
			return fmt.Sprintf("SELECT %s, ROW_NUMBER() OVER (ORDER BY %s) AS rn FROM %s",
				firstCol.Name, firstCol.Name, t.Name)
		}))

		// --- CTE (Common Table Expression) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("cte", []string{"cte"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("WITH cte AS (SELECT %s, COUNT(*) AS cnt FROM %s GROUP BY %s) SELECT * FROM cte WHERE cnt > 1",
				firstCol.Name, t.Name, firstCol.Name)
		}))

		// --- Recursive CTE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("recursive_cte", []string{"recursive-cte"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("WITH RECURSIVE seq AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM seq WHERE n < 100) SELECT s.n, t.%s FROM seq s JOIN %s t ON t.%s = s.n",
				firstCol.Name, t.Name, firstCol.Name)
		}))

		// --- INSERT ... ON DUPLICATE KEY UPDATE (MySQL-specific) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("insert_on_dup", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s = VALUES(%s)",
				t.Name, tc.Name, colLiteral(tc, rng), tc.Name, tc.Name)
		}))

		// --- REPLACE INTO (MySQL-specific) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("replace_into", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("REPLACE INTO %s (%s) VALUES (%s)",
				t.Name, tc.Name, colLiteral(tc, rng))
		}))

		// --- INSERT IGNORE (MySQL-specific) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("insert_ignore", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (%s)",
				t.Name, tc.Name, colLiteral(tc, rng))
		}))

		// --- GROUP_CONCAT (MySQL-specific) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("group_concat", []string{"expensive-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, GROUP_CONCAT(%s) FROM %s GROUP BY %s",
				firstCol.Name, tc.Name, t.Name, firstCol.Name)
		}))

		// --- JSON functions (MySQL-specific) ---
		jsonCols := jsonColDefs(t)
		if len(jsonCols) > 0 {
			jc := jsonCols[0]
			tmpls = append(tmpls, calibrate.NewQueryTempl("json_extract", []string{"expensive-function"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT JSON_EXTRACT(%s, '$.key') FROM %s WHERE %s = %s",
					jc.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("json_arrow", []string{"expensive-function"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT %s->>'$.key' FROM %s WHERE %s = %s",
					jc.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("json_contains", []string{"non-sargable"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE JSON_CONTAINS(%s, '1', '$.key')",
					t.Name, jc.Name)
			}))
		}

		// --- FIND_IN_SET (MySQL-specific) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("find_in_set", []string{"non-sargable"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE FIND_IN_SET(%s, 'a,b,c,d') > 0",
				t.Name, tc.Name)
		}))

		// --- Implicit cast in predicate ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("implicit_cast", []string{"implicit-cast-in-predicate"}, func(rng *rand.Rand) string {
			// Compare varchar to int — forces implicit cast
			return fmt.Sprintf("SELECT * FROM %s WHERE %s = %d",
				t.Name, tc.Name, rng.Intn(1000))
		}))

		// --- Multi-table UPDATE (MySQL-specific) ---
		for _, fk := range d.ForeignKeys {
			if fk.Table == t.Name {
				fkCopy := fk
				tmpls = append(tmpls, calibrate.NewQueryTempl("multi_table_update", []string{"missing-where-clause"}, func(rng *rand.Rand) string {
					return fmt.Sprintf("UPDATE %s a JOIN %s b ON a.%s = b.%s SET a.%s = %s",
						fkCopy.Table, fkCopy.RefTable, fkCopy.Column, fkCopy.RefColumn,
						tc.Name, colLiteral(tc, rng))
				}))
				break
			}
		}

		// --- Multi-table DELETE (MySQL-specific) ---
		for _, fk := range d.ForeignKeys {
			if fk.Table == t.Name {
				fkCopy := fk
				tmpls = append(tmpls, calibrate.NewQueryTempl("multi_table_delete", []string{"missing-where-clause"}, func(rng *rand.Rand) string {
					return fmt.Sprintf("DELETE a FROM %s a JOIN %s b ON a.%s = b.%s WHERE b.%s = %s",
						fkCopy.Table, fkCopy.RefTable, fkCopy.Column, fkCopy.RefColumn,
						firstCol.Name, colLiteral(firstCol, rng))
				}))
				break
			}
		}

		// --- DDL statements ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("create_table", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("CREATE TABLE IF NOT EXISTS temp_%s_%d (id INT AUTO_INCREMENT PRIMARY KEY, val VARCHAR(100)) ENGINE=InnoDB",
				t.Name, rng.Intn(10000))
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("alter_table", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS temp_col_%d VARCHAR(50)",
				t.Name, rng.Intn(10000))
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("drop_table", []string{"ddl-statement", "cascade-drop"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("DROP TABLE IF EXISTS temp_%s_%d",
				t.Name, rng.Intn(10000))
		}))

		// --- Volatile functions ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("volatile_func", []string{"volatile-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT *, UUID() AS uid FROM %s WHERE %s = %s",
				t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("volatile_rand", []string{"volatile-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE RAND() < 0.1", t.Name)
		}))

		// --- Expensive functions ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("expensive_concat", []string{"expensive-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT CONCAT(%s, '-', %s) AS combined FROM %s",
				tc.Name, firstCol.Name, t.Name)
		}))
		tmpls = append(tmpls, calibrate.NewQueryTempl("expensive_substr", []string{"expensive-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s WHERE SUBSTRING(%s, 1, 3) = 'abc'",
				t.Name, tc.Name)
		}))
	}

	// =========================================================================
	// Stored Procedure and Function patterns
	// =========================================================================
	// These generate CALL and function-related statements to test sproc scoring.

	for _, table := range tables {
		t := table
		colDefs := nonSerialColDefs(t)
		cols := nonSerialCols(t)
		if len(cols) == 0 || len(colDefs) == 0 {
			continue
		}
		firstCol := colDefs[0]
		tc := textCol(colDefs)

		// --- CREATE PROCEDURE ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("create_proc", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			pname := fmt.Sprintf("sp_%s_%d", t.Name, rng.Intn(10000))
			return fmt.Sprintf("CREATE PROCEDURE %s() BEGIN SELECT * FROM %s; END", pname, t.Name)
		}))

		// --- CREATE FUNCTION ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("create_func", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			fname := fmt.Sprintf("fn_%s_%d", t.Name, rng.Intn(10000))
			return fmt.Sprintf("CREATE FUNCTION %s(p INT) RETURNS INT DETERMINISTIC BEGIN RETURN p * 2; END", fname)
		}))

		// --- SELECT with user-defined function call ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("udf_in_select", []string{"expensive-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, CONCAT(UPPER(%s), LOWER(%s)) AS transformed FROM %s WHERE %s = %s",
				firstCol.Name, tc.Name, tc.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))

		// --- Nested function calls (expensive) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("nested_funcs", []string{"expensive-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT REVERSE(UPPER(TRIM(%s))) AS val FROM %s WHERE %s = %s",
				tc.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
		}))

		// --- Function in ORDER BY (non-sargable sort) ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("func_in_order", []string{"non-sargable", "unbounded-sort"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s ORDER BY LENGTH(%s)", t.Name, tc.Name)
		}))

		// --- Function in GROUP BY ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("func_in_group", []string{"non-sargable", "group-by-fanout"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT LEFT(%s, 3), COUNT(*) FROM %s GROUP BY LEFT(%s, 3)",
				tc.Name, t.Name, tc.Name)
		}))

		// --- Aggregate with HAVING using function ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("having_func", []string{"group-by-fanout", "expensive-function"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, GROUP_CONCAT(%s ORDER BY %s) FROM %s GROUP BY %s HAVING COUNT(*) > %d",
				firstCol.Name, tc.Name, tc.Name, t.Name, firstCol.Name, rng.Intn(3)+1)
		}))

		// --- Date/time functions ---
		dateCols := dateColDefs(t)
		if len(dateCols) > 0 {
			dc := dateCols[0]
			tmpls = append(tmpls, calibrate.NewQueryTempl("date_func_where", []string{"non-sargable"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE YEAR(%s) = 2025", t.Name, dc.Name)
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("date_func_select", []string{"expensive-function"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT %s, DATEDIFF(NOW(), %s) AS days_ago FROM %s WHERE %s = %s",
					dc.Name, dc.Name, t.Name, firstCol.Name, colLiteral(firstCol, rng))
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("date_range", nil, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE %s BETWEEN '2024-01-01' AND '2025-12-31'",
					t.Name, dc.Name)
			}))
		}

		// --- Conditional aggregation ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("cond_agg", []string{"case-expression", "group-by-fanout"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT %s, SUM(CASE WHEN %s IS NOT NULL THEN 1 ELSE 0 END) AS non_null_count FROM %s GROUP BY %s",
				firstCol.Name, tc.Name, t.Name, firstCol.Name)
		}))

		// --- EXISTS vs IN performance comparison ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("exists_subquery", []string{"correlated-subquery"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s a WHERE EXISTS (SELECT 1 FROM %s b WHERE b.%s = a.%s AND b.%s IS NOT NULL)",
				t.Name, t.Name, firstCol.Name, firstCol.Name, tc.Name)
		}))

		// --- NOT EXISTS ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("not_exists", []string{"correlated-subquery"}, func(rng *rand.Rand) string {
			return fmt.Sprintf("SELECT * FROM %s a WHERE NOT EXISTS (SELECT 1 FROM %s b WHERE b.%s = a.%s AND b.%s IS NULL)",
				t.Name, t.Name, firstCol.Name, firstCol.Name, tc.Name)
		}))
	}

	// =========================================================================
	// Storage Engine Variation
	// =========================================================================
	// MySQL supports multiple storage engines with different performance
	// characteristics. These templates create and query tables with different
	// engines to measure the cost impact.

	for _, table := range tables {
		t := table
		colDefs := nonSerialColDefs(t)
		cols := nonSerialCols(t)
		if len(cols) == 0 || len(colDefs) == 0 {
			continue
		}
		firstCol := colDefs[0]

		// --- CREATE TABLE with different engines ---
		for _, engine := range []string{"MyISAM", "MEMORY", "ARCHIVE"} {
			eng := engine
			tmpls = append(tmpls, calibrate.NewQueryTempl("create_"+strings.ToLower(eng), []string{"ddl-statement"}, func(rng *rand.Rand) string {
				tname := fmt.Sprintf("tmp_%s_%s_%d", strings.ToLower(eng), t.Name, rng.Intn(10000))
				return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INT AUTO_INCREMENT PRIMARY KEY, val VARCHAR(100), num INT) ENGINE=%s",
					tname, eng)
			}))
		}

		// --- ALTER TABLE to change engine ---
		tmpls = append(tmpls, calibrate.NewQueryTempl("alter_engine", []string{"ddl-statement"}, func(rng *rand.Rand) string {
			engines := []string{"InnoDB", "MyISAM"}
			return fmt.Sprintf("ALTER TABLE %s ENGINE=%s", t.Name, engines[rng.Intn(len(engines))])
		}))

		// --- FORCE INDEX / USE INDEX / IGNORE INDEX (MySQL optimizer hints) ---
		if len(d.Indexes) > 0 {
			idx := d.Indexes[0]
			idxCopy := idx
			tmpls = append(tmpls, calibrate.NewQueryTempl("force_index", nil, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s FORCE INDEX (%s) WHERE %s = %s",
					idxCopy.Table, idxCopy.Name, firstCol.Name, colLiteral(firstCol, rng))
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("ignore_index", []string{"non-sargable"}, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s IGNORE INDEX (%s) WHERE %s = %s",
					idxCopy.Table, idxCopy.Name, firstCol.Name, colLiteral(firstCol, rng))
			}))
			tmpls = append(tmpls, calibrate.NewQueryTempl("use_index", nil, func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s USE INDEX (%s) WHERE %s = %s",
					idxCopy.Table, idxCopy.Name, firstCol.Name, colLiteral(firstCol, rng))
			}))
		}

		// --- STRAIGHT_JOIN (MySQL-specific optimizer hint) ---
		for _, fk := range d.ForeignKeys {
			if fk.Table == t.Name {
				fkCopy := fk
				tmpls = append(tmpls, calibrate.NewQueryTempl("straight_join", []string{"join"}, func(rng *rand.Rand) string {
					return fmt.Sprintf("SELECT STRAIGHT_JOIN a.*, b.* FROM %s a JOIN %s b ON a.%s = b.%s WHERE a.%s = %s",
						fkCopy.Table, fkCopy.RefTable, fkCopy.Column, fkCopy.RefColumn,
						firstCol.Name, colLiteral(firstCol, rng))
				}))
				break
			}
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

func jsonColDefs(t calibrate.TableDef) []calibrate.ColumnDef {
	var cols []calibrate.ColumnDef
	for _, c := range t.Columns {
		if c.Type == "JSON" {
			cols = append(cols, c)
		}
	}
	return cols
}

func dateColDefs(t calibrate.TableDef) []calibrate.ColumnDef {
	var cols []calibrate.ColumnDef
	for _, c := range t.Columns {
		if c.Type == "DATE" || c.Type == "DATETIME" {
			cols = append(cols, c)
		}
	}
	return cols
}
