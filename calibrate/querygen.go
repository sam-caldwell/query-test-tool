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
					fn := nonsargableFunc(col.Type, rng)
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

		// 14a. LEFT JOIN (outer join)
		tmpls = append(tmpls, queryTempl{
			queryType: "left_join",
			rules:     []string{"outer-join"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s LEFT JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14b. RIGHT JOIN (outer join)
		tmpls = append(tmpls, queryTempl{
			queryType: "right_join",
			rules:     []string{"outer-join"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s RIGHT JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14c. FULL JOIN (outer join)
		tmpls = append(tmpls, queryTempl{
			queryType: "full_join",
			rules:     []string{"outer-join"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s FULL JOIN %s ON %s.%s = %s.id LIMIT 100",
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

	// --- New rule templates ---

	// 18a. COALESCE in WHERE (null-coalesce-in-predicate)
	for _, table := range tables {
		t := table
		// Find nullable columns
		var nullableCols []ColumnDef
		for _, c := range t.Columns {
			if !c.IsSerial && !c.NotNull {
				nullableCols = append(nullableCols, c)
			}
		}
		if len(nullableCols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "coalesce_predicate",
				rules:     []string{"null-coalesce-in-predicate"},
				gen: func(rng *rand.Rand) string {
					col := nullableCols[rng.Intn(len(nullableCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE COALESCE(%s, %s) = %s",
						t.Name, col.Name, sampleValue(col.Type, rng), sampleValue(col.Type, rng))
				},
			})
		}

		cols := nonSerialCols(t)

		// 18b. Null check chain (3+ IS NULL checks)
		if len(nullableCols) >= 3 {
			tmpls = append(tmpls, queryTempl{
				queryType: "null_check_chain",
				rules:     []string{"null-check-chain"},
				gen: func(rng *rand.Rand) string {
					var checks []string
					used := make(map[int]bool)
					for len(checks) < 3 && len(used) < len(nullableCols) {
						idx := rng.Intn(len(nullableCols))
						if used[idx] {
							continue
						}
						used[idx] = true
						if rng.Intn(2) == 0 {
							checks = append(checks, fmt.Sprintf("%s IS NULL", nullableCols[idx].Name))
						} else {
							checks = append(checks, fmt.Sprintf("%s IS NOT NULL", nullableCols[idx].Name))
						}
					}
					return fmt.Sprintf("SELECT * FROM %s WHERE %s",
						t.Name, strings.Join(checks, " AND "))
				},
			})
		}

		// 18c. LIKE with leading wildcard
		var textCols []ColumnDef
		for _, c := range cols {
			if strings.HasPrefix(c.Type, "VARCHAR") || c.Type == "TEXT" {
				textCols = append(textCols, c)
			}
		}
		if len(textCols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "like_leading_wildcard",
				rules:     []string{"like-leading-wildcard"},
				gen: func(rng *rand.Rand) string {
					col := textCols[rng.Intn(len(textCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s LIKE '%%value_%d'",
						t.Name, col.Name, rng.Intn(100))
				},
			})
		}

		// 18d. Implicit cast in predicate
		var intCols []ColumnDef
		for _, c := range cols {
			if c.Type == "INT" || c.Type == "BIGINT" || c.Type == "SMALLINT" {
				intCols = append(intCols, c)
			}
		}
		if len(intCols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "implicit_cast",
				rules:     []string{"implicit-cast-in-predicate"},
				gen: func(rng *rand.Rand) string {
					col := intCols[rng.Intn(len(intCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s::text = '%d'",
						t.Name, col.Name, rng.Intn(1000))
				},
			})
		}

		// 18e. Large OFFSET
		tmpls = append(tmpls, queryTempl{
			queryType: "large_offset",
			rules:     []string{"large-offset"},
			gen: func(rng *rand.Rand) string {
				offset := 200 + rng.Intn(800)
				return fmt.Sprintf("SELECT * FROM %s ORDER BY id LIMIT 10 OFFSET %d", t.Name, offset)
			},
		})

		// 18f. Large IN list (25+ values)
		tmpls = append(tmpls, queryTempl{
			queryType: "large_in_list",
			rules:     []string{"large-in-list"},
			gen: func(rng *rand.Rand) string {
				n := 25 + rng.Intn(25)
				vals := make([]string, n)
				for i := range vals {
					vals[i] = fmt.Sprintf("%d", rng.Intn(10000))
				}
				return fmt.Sprintf("SELECT * FROM %s WHERE id IN (%s)",
					t.Name, strings.Join(vals, ","))
			},
		})

		// 18g. FOR UPDATE lock
		tmpls = append(tmpls, queryTempl{
			queryType: "for_update",
			rules:     []string{"for-update-lock"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE id = %d FOR UPDATE",
					t.Name, 1+rng.Intn(1000))
			},
		})

		// 18h. DELETE without WHERE (missing-where-clause)
		tmpls = append(tmpls, queryTempl{
			queryType: "delete_no_where",
			rules:     []string{"missing-where-clause"},
			gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("DELETE FROM %s", t.Name)
			},
		})

		// 18i. UPDATE without WHERE (missing-where-clause)
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "update_no_where",
				rules:     []string{"missing-where-clause"},
				gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					return fmt.Sprintf("UPDATE %s SET %s = %s",
						t.Name, col.Name, sampleValue(col.Type, rng))
				},
			})
		}

		// 18j. INSERT...RETURNING
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				queryType: "insert_returning",
				rules:     []string{"returning-clause"},
				gen: func(rng *rand.Rand) string {
					col := cols[0]
					return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
						t.Name, col.Name, sampleValue(col.Type, rng))
				},
			})
		}

		// 18k. GROUP BY ROLLUP (grouping-sets)
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				queryType: "grouping_sets",
				rules:     []string{"grouping-sets"},
				gen: func(rng *rand.Rand) string {
					c1 := cols[rng.Intn(len(cols))]
					c2 := cols[rng.Intn(len(cols))]
					if c1.Name == c2.Name && len(cols) > 1 {
						c2 = cols[(rng.Intn(len(cols))+1)%len(cols)]
					}
					rollupOrCube := "ROLLUP"
					if rng.Intn(2) == 0 {
						rollupOrCube = "CUBE"
					}
					return fmt.Sprintf("SELECT %s, %s, COUNT(*) FROM %s GROUP BY %s(%s, %s)",
						c1.Name, c2.Name, t.Name, rollupOrCube, c1.Name, c2.Name)
				},
			})
		}

		// 18l. Expensive function (regexp, string_agg)
		if len(textCols) > 0 {
			tmpls = append(tmpls, queryTempl{
				queryType: "expensive_func",
				rules:     []string{"expensive-function"},
				gen: func(rng *rand.Rand) string {
					col := textCols[rng.Intn(len(textCols))]
					patterns := []string{
						fmt.Sprintf("SELECT * FROM %s WHERE %s ~ '^[A-Z].*%d'", t.Name, col.Name, rng.Intn(100)),
						fmt.Sprintf("SELECT string_agg(%s, ', ') FROM %s GROUP BY id %% 10", col.Name, t.Name),
						fmt.Sprintf("SELECT regexp_replace(%s, '[0-9]+', 'X', 'g') FROM %s LIMIT 100", col.Name, t.Name),
					}
					return patterns[rng.Intn(len(patterns))]
				},
			})
		}

		// 18m. Volatile function in predicate
		tmpls = append(tmpls, queryTempl{
			queryType: "volatile_func",
			rules:     []string{"volatile-function"},
			gen: func(rng *rand.Rand) string {
				patterns := []string{
					fmt.Sprintf("SELECT * FROM %s WHERE random() > 0.5", t.Name),
					fmt.Sprintf("SELECT * FROM %s WHERE created_at > now() - interval '1 day'", t.Name),
				}
				return patterns[rng.Intn(len(patterns))]
			},
		})
	}

	// 18n. Recursive CTE
	if len(tables) > 0 {
		tmpls = append(tmpls, queryTempl{
			queryType: "recursive_cte",
			rules:     []string{"recursive-cte"},
			gen: func(rng *rand.Rand) string {
				t := tables[rng.Intn(len(tables))]
				return fmt.Sprintf(
					"WITH RECURSIVE tree AS (SELECT id, 1 AS depth FROM %s WHERE id <= 10 UNION ALL SELECT t.id, tree.depth + 1 FROM %s t JOIN tree ON t.id = tree.id + 1 WHERE tree.depth < 5) SELECT * FROM tree",
					t.Name, t.Name)
			},
		})
	}

	// 18o. LATERAL join
	if len(joins) > 0 {
		tmpls = append(tmpls, queryTempl{
			queryType: "lateral_join",
			rules:     []string{"lateral-join"},
			gen: func(rng *rand.Rand) string {
				j := joins[rng.Intn(len(joins))]
				return fmt.Sprintf(
					"SELECT * FROM %s p, LATERAL (SELECT * FROM %s c WHERE c.%s = p.id ORDER BY c.id LIMIT 3) sub",
					j.right, j.left, j.leftCol)
			},
		})
	}

	// 18p. Multi-join (3+ joins for join-count-squared)
	if len(joins) >= 3 {
		tmpls = append(tmpls, queryTempl{
			queryType: "multi_join",
			rules:     nil, // triggers join + join-count-squared via count
			gen: func(rng *rand.Rand) string {
				// Pick 3-4 random join pairs and chain them
				n := 3 + rng.Intn(min(3, len(joins)-2))
				if n > len(joins) {
					n = len(joins)
				}
				sql := fmt.Sprintf("SELECT * FROM %s", joins[0].right)
				for i := 0; i < n; i++ {
					j := joins[i%len(joins)]
					sql += fmt.Sprintf(" JOIN %s ON %s.%s = %s.id",
						j.left, j.left, j.leftCol, j.right)
				}
				sql += " LIMIT 100"
				return sql
			},
		})
	}

	// 18q. UNION without ALL (union-distinct)
	if len(tables) >= 2 {
		tmpls = append(tmpls, queryTempl{
			queryType: "union_distinct",
			rules:     []string{"union-distinct"},
			gen: func(rng *rand.Rand) string {
				t1 := tables[rng.Intn(len(tables))]
				cols1 := nonSerialCols(t1)
				if len(cols1) == 0 {
					return fmt.Sprintf("SELECT 1 FROM %s UNION SELECT 1 FROM %s", t1.Name, t1.Name)
				}
				col := cols1[0]
				return fmt.Sprintf("SELECT %s FROM %s UNION SELECT %s FROM %s",
					col.Name, t1.Name, col.Name, t1.Name)
			},
		})
	}

	// 18r. DDL statement
	tmpls = append(tmpls, queryTempl{
		queryType: "ddl_create",
		rules:     []string{"ddl-statement"},
		gen: func(rng *rand.Rand) string {
			tmpName := fmt.Sprintf("tmp_cal_%d", rng.Intn(100000))
			return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id serial primary key, val text)", tmpName)
		},
	})

	// 18s. CASCADE DROP
	tmpls = append(tmpls, queryTempl{
		queryType: "cascade_drop",
		rules:     []string{"cascade-drop", "ddl-statement"},
		gen: func(rng *rand.Rand) string {
			tmpName := fmt.Sprintf("tmp_cal_%d", rng.Intn(100000))
			return fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", tmpName)
		},
	})

	// 18t. JSONB operations (serialize/deserialize patterns)
	for _, table := range tables {
		t := table
		var jsonbCols []ColumnDef
		for _, c := range t.Columns {
			if c.Type == "JSONB" {
				jsonbCols = append(jsonbCols, c)
			}
		}
		if len(jsonbCols) > 0 {
			// JSON field access in WHERE (containment query)
			tmpls = append(tmpls, queryTempl{
				queryType: "jsonb_containment",
				rules:     []string{"expensive-function"},
				gen: func(rng *rand.Rand) string {
					col := jsonbCols[rng.Intn(len(jsonbCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s @> '{\"key\": %d}'",
						t.Name, col.Name, rng.Intn(1000))
				},
			})

			// JSON field extraction in SELECT (deserialization)
			tmpls = append(tmpls, queryTempl{
				queryType: "jsonb_extract",
				rules:     []string{"expensive-function"},
				gen: func(rng *rand.Rand) string {
					col := jsonbCols[rng.Intn(len(jsonbCols))]
					return fmt.Sprintf("SELECT id, %s->>'key' AS key_val, %s->'value' AS val_obj FROM %s WHERE %s IS NOT NULL LIMIT 100",
						col.Name, col.Name, t.Name, col.Name)
				},
			})

			// JSON path query (PG12+)
			tmpls = append(tmpls, queryTempl{
				queryType: "jsonb_path",
				rules:     []string{"expensive-function"},
				gen: func(rng *rand.Rand) string {
					col := jsonbCols[rng.Intn(len(jsonbCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE jsonb_path_exists(%s, '$.key ? (@ > %d)')",
						t.Name, col.Name, rng.Intn(100))
				},
			})

			// JSON aggregation (building JSON from rows)
			tmpls = append(tmpls, queryTempl{
				queryType: "jsonb_agg",
				rules:     []string{"expensive-function"},
				gen: func(rng *rand.Rand) string {
					cols := nonSerialCols(t)
					if len(cols) < 2 {
						return fmt.Sprintf("SELECT jsonb_agg(to_jsonb(id)) FROM %s", t.Name)
					}
					c := cols[rng.Intn(len(cols))]
					return fmt.Sprintf("SELECT jsonb_agg(jsonb_build_object('id', id, '%s', %s)) FROM %s LIMIT 100",
						c.Name, c.Name, t.Name)
				},
			})
		}
	}

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

func nonsargableFunc(colType string, rng *rand.Rand) string {
	switch {
	case strings.HasPrefix(colType, "VARCHAR") || colType == "TEXT":
		funcs := []string{"LOWER", "UPPER", "TRIM", "LENGTH"}
		return funcs[rng.Intn(len(funcs))]
	case colType == "INT" || colType == "BIGINT" || colType == "SMALLINT":
		return "ABS"
	case strings.HasPrefix(colType, "NUMERIC"):
		funcs := []string{"ROUND", "FLOOR", "CEIL"}
		return funcs[rng.Intn(len(funcs))]
	case colType == "DATE" || colType == "TIMESTAMPTZ" || colType == "TIMESTAMP":
		return "EXTRACT"
	default:
		return "LENGTH"
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
