package calibrate

import (
	"fmt"
	"math/rand"
	"strings"
)

// QueryGenerator produces random queries targeting specific antipatterns.
type QueryGenerator struct {
	rng *rand.Rand
}

// NewQueryGenerator creates a new query generator.
func NewQueryGenerator(seed int64) *QueryGenerator {
	return &QueryGenerator{rng: rand.New(rand.NewSource(seed))}
}

// GenerateQueries produces queries for a schema family targeting total query count.
func (qg *QueryGenerator) GenerateQueries(domain Domain, familyID int, count int) []GeneratedQuery {
	templates := qg.allTemplates(domain)
	if len(templates) == 0 {
		return nil
	}

	queries := make([]GeneratedQuery, 0, count)
	for i := 0; i < count; i++ {
		tmpl := templates[qg.rng.Intn(len(templates))]
		sql := tmpl.gen(qg.rng)
		queries = append(queries, GeneratedQuery{
			FamilyID:    familyID,
			SQL:         sql,
			QueryType:   tmpl.queryType,
			TargetRules: tmpl.rules,
		})
	}
	return queries
}

type queryTempl struct {
	queryType string
	rules     []string
	gen       func(rng *rand.Rand) string
}

func (qg *QueryGenerator) allTemplates(d Domain) []queryTempl {
	var tmpls []queryTempl

	tables := d.Tables
	if len(tables) == 0 {
		return nil
	}

	// Find tables with relationships
	type joinPair struct {
		left, right, leftCol, rightCol string
	}
	var joins []joinPair
	for _, fk := range d.ForeignKeys {
		joins = append(joins, joinPair{fk.Table, fk.RefTable, fk.Column, fk.RefColumn})
	}

	// Helper: pick a non-serial, non-PK column from a table
	nonSerialCols := func(t TableDef) []ColumnDef {
		var cols []ColumnDef
		for _, c := range t.Columns {
			if !c.IsSerial {
				cols = append(cols, c)
			}
		}
		return cols
	}

	// Helper: pick indexed columns
	indexedCols := func(t TableDef) []string {
		var cols []string
		for _, idx := range d.Indexes {
			if idx.Table == t.Name && len(idx.Columns) > 0 {
				cols = append(cols, idx.Columns[0])
			}
		}
		return cols
	}

	for _, table := range tables {
		t := table // capture
		cols := nonSerialCols(t)
		idxCols := indexedCols(t)

		// 1. SELECT *
		tmpls = append(tmpls, queryTempl{
			queryType: "select_star",
			rules:     []string{"select-star"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s LIMIT 100", t.Name)
			},
		})

		// 2. SELECT specific columns (control)
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				queryType: "select_columns",
				rules:     nil,
				gen: func(rng *rand.Rand) string {
					n := 2 + rng.Intn(min(3, len(cols)-1))
					var selected []string
					for i := 0; i < n && i < len(cols); i++ {
						selected = append(selected, cols[i].Name)
					}
					return fmt.Sprintf("SELECT %s FROM %s LIMIT 100", strings.Join(selected, ", "), t.Name)
				},
			})
		}

		// 3. Non-sargable WHERE (function on column)
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "non_sargable",
				rules:     []string{"non-sargable"},
				gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					fn := nonsargableFunc(col.Type)
					return fmt.Sprintf("SELECT * FROM %s WHERE %s(%s) = %s",
						t.Name, fn, col.Name, sampleValue(col.Type, rng))
				},
			})
		}

		// 4. Sargable WHERE (direct comparison — control)
		if len(idxCols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "sargable",
				rules:     nil,
				gen: func(rng *rand.Rand) string {
					col := idxCols[rng.Intn(len(idxCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s = %d", t.Name, col, 1+rng.Intn(1000))
				},
			})
		}

		// 5. Unbounded ORDER BY
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "unbounded_sort",
				rules:     []string{"unbounded-sort"},
				gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					return fmt.Sprintf("SELECT * FROM %s ORDER BY %s", t.Name, col.Name)
				},
			})
		}

		// 6. Bounded ORDER BY (control)
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "bounded_sort",
				rules:     nil,
				gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					limit := 10 + rng.Intn(90)
					return fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT %d", t.Name, col.Name, limit)
				},
			})
		}

		// 7. GROUP BY with aggregation
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				queryType: "group_by",
				rules:     []string{"group-by-fanout"},
				gen: func(rng *rand.Rand) string {
					groupCol := cols[rng.Intn(len(cols))]
					aggs := []string{"COUNT(*)", "SUM(1)", "AVG(1)"}
					agg := aggs[rng.Intn(len(aggs))]
					return fmt.Sprintf("SELECT %s, %s FROM %s GROUP BY %s",
						groupCol.Name, agg, t.Name, groupCol.Name)
				},
			})
		}

		// 8. Window function without PARTITION BY
		tmpls = append(tmpls, queryTempl{
			queryType: "window_no_partition",
			rules:     []string{"window-function"},
			gen: func(rng *rand.Rand) string {
				col := cols[rng.Intn(len(cols))]
				return fmt.Sprintf("SELECT %s, row_number() OVER (ORDER BY %s) FROM %s",
					col.Name, col.Name, t.Name)
			},
		})

		// 9. Window function with PARTITION BY
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				queryType: "window_partitioned",
				rules:     []string{"window-function"},
				gen: func(rng *rand.Rand) string {
					partCol := cols[rng.Intn(len(cols))]
					orderCol := cols[rng.Intn(len(cols))]
					return fmt.Sprintf("SELECT %s, row_number() OVER (PARTITION BY %s ORDER BY %s) FROM %s",
						partCol.Name, partCol.Name, orderCol.Name, t.Name)
				},
			})
		}

		// 10. EXISTS subquery
		if len(joins) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "exists_subquery",
				rules:     []string{"correlated-subquery"},
				gen: func(rng *rand.Rand) string {
					j := joins[rng.Intn(len(joins))]
					return fmt.Sprintf(
						"SELECT * FROM %s WHERE EXISTS (SELECT 1 FROM %s WHERE %s.%s = %s.id)",
						j.right, j.left, j.left, j.leftCol, j.right)
				},
			})
		}

		// 11. IN subquery
		if len(joins) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "in_subquery",
				rules:     []string{"correlated-subquery"},
				gen: func(rng *rand.Rand) string {
					j := joins[rng.Intn(len(joins))]
					return fmt.Sprintf(
						"SELECT * FROM %s WHERE id IN (SELECT %s FROM %s)",
						j.right, j.leftCol, j.left)
				},
			})
		}

		// 12. CASE expression
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "case_expr",
				rules:     []string{"case-expression"},
				gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					return fmt.Sprintf(
						"SELECT %s, CASE WHEN %s IS NULL THEN 'null' WHEN %s = %s THEN 'match' ELSE 'other' END FROM %s",
						col.Name, col.Name, col.Name, sampleValue(col.Type, rng), t.Name)
				},
			})
		}
	}

	// Multi-table templates
	for _, j := range joins {
		jCopy := j

		// 13. Proper JOIN
		tmpls = append(tmpls, queryTempl{
			queryType: "proper_join",
			rules:     nil,
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14. DISTINCT with JOIN (dedup)
		tmpls = append(tmpls, queryTempl{
			queryType: "distinct_join",
			rules:     []string{"distinct-dedup"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT DISTINCT %s.* FROM %s JOIN %s ON %s.%s = %s.id",
					jCopy.right, jCopy.right, jCopy.left, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})
	}

	// 15. Cartesian product (implicit cross join)
	if len(tables) >= 2 {
		tmpls = append(tmpls, queryTempl{
			queryType: "cartesian",
			rules:     []string{"cartesian-product", "missing-predicate"},
			gen: func(rng *rand.Rand) string {
				t1 := tables[rng.Intn(len(tables))]
				t2 := tables[rng.Intn(len(tables))]
				if t1.Name == t2.Name {
					t2 = tables[(rng.Intn(len(tables))+1)%len(tables)]
				}
				return fmt.Sprintf("SELECT * FROM %s, %s LIMIT 100", t1.Name, t2.Name)
			},
		})
	}

	// 16. UNION
	if len(tables) >= 2 {
		tmpls = append(tmpls, queryTempl{
			queryType: "union",
			rules:     []string{"set-operation"},
			gen: func(rng *rand.Rand) string {
				t1 := tables[rng.Intn(len(tables))]
				cols1 := nonSerialCols(t1)
				if len(cols1) == 0 {
					return fmt.Sprintf("SELECT 1 FROM %s UNION SELECT 1 FROM %s", t1.Name, t1.Name)
				}
				col := cols1[0]
				return fmt.Sprintf("SELECT %s FROM %s UNION ALL SELECT %s FROM %s",
					col.Name, t1.Name, col.Name, t1.Name)
			},
		})
	}

	// 17. CTE
	tmpls = append(tmpls, queryTempl{
		queryType: "cte",
		rules:     []string{"cte"},
		gen: func(rng *rand.Rand) string {
			t := tables[rng.Intn(len(tables))]
			return fmt.Sprintf(
				"WITH cte AS (SELECT * FROM %s LIMIT 100) SELECT * FROM cte",
				t.Name)
		},
	})

	// 18. Nested boolean
	if len(tables) > 0 {
		tmpls = append(tmpls, queryTempl{
			queryType: "boolean_nesting",
			rules:     []string{"boolean-nesting"},
			gen: func(rng *rand.Rand) string {
				t := tables[rng.Intn(len(tables))]
				tCols := nonSerialCols(t)
				if len(tCols) < 2 {
					return fmt.Sprintf("SELECT * FROM %s WHERE (%s IS NOT NULL AND %s IS NOT NULL) OR %s IS NULL",
						t.Name, tCols[0].Name, tCols[0].Name, tCols[0].Name)
				}
				c1 := tCols[rng.Intn(len(tCols))]
				c2 := tCols[rng.Intn(len(tCols))]
				return fmt.Sprintf("SELECT * FROM %s WHERE (%s IS NOT NULL AND %s IS NOT NULL) OR (%s IS NULL AND %s IS NULL)",
					t.Name, c1.Name, c2.Name, c1.Name, c2.Name)
			},
		})
	}

	return tmpls
}

func nonsargableFunc(colType string) string {
	switch {
	case strings.HasPrefix(colType, "VARCHAR") || colType == "TEXT":
		funcs := []string{"LOWER", "UPPER", "TRIM", "LENGTH"}
		return funcs[rand.Intn(len(funcs))]
	case colType == "INT" || colType == "BIGINT" || colType == "SMALLINT":
		return "ABS"
	case strings.HasPrefix(colType, "NUMERIC"):
		funcs := []string{"ROUND", "FLOOR", "CEIL"}
		return funcs[rand.Intn(len(funcs))]
	case colType == "DATE" || colType == "TIMESTAMPTZ" || colType == "TIMESTAMP":
		return "DATE_TRUNC"
	default:
		return "CAST"
	}
}

func sampleValue(colType string, rng *rand.Rand) string {
	switch {
	case strings.HasPrefix(colType, "VARCHAR") || colType == "TEXT":
		return fmt.Sprintf("'value_%d'", rng.Intn(1000))
	case colType == "INT" || colType == "BIGINT" || colType == "SMALLINT":
		return fmt.Sprintf("%d", rng.Intn(1000))
	case strings.HasPrefix(colType, "NUMERIC"):
		return fmt.Sprintf("%d.%02d", rng.Intn(1000), rng.Intn(100))
	case colType == "BOOLEAN":
		if rng.Intn(2) == 0 {
			return "true"
		}
		return "false"
	case colType == "DATE":
		return "'2024-01-15'"
	case colType == "TIMESTAMPTZ", colType == "TIMESTAMP":
		return "'2024-01-15 10:00:00'"
	default:
		return "'test'"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
