package calibrate

import (
	"fmt"
	"math/rand"
	"strings"
)

// QueryGenerator produces random queries targeting specific antipatterns.
type QueryGenerator struct {
	rng         *rand.Rand
	templatesFn func(d Domain) []queryTempl // custom templates, nil = use default PG
}

// NewQueryGenerator creates a new query generator with default PostgreSQL templates.
func NewQueryGenerator(seed int64) *QueryGenerator {
	return &QueryGenerator{rng: rand.New(rand.NewSource(seed))}
}

// NewQueryGeneratorWithTemplates creates a query generator with custom templates.
func NewQueryGeneratorWithTemplates(seed int64, templatesFn func(d Domain) []queryTempl) *QueryGenerator {
	return &QueryGenerator{
		rng:         rand.New(rand.NewSource(seed)),
		templatesFn: templatesFn,
	}
}

// GenerateQueries produces queries for a schema family targeting total query count.
func (qg *QueryGenerator) GenerateQueries(domain Domain, familyID int, count int) []GeneratedQuery {
	var templates []queryTempl
	if qg.templatesFn != nil {
		templates = qg.templatesFn(domain)
	} else {
		templates = qg.allTemplates(domain)
	}
	if len(templates) == 0 {
		return nil
	}

	queries := make([]GeneratedQuery, 0, count)
	for i := 0; i < count; i++ {
		tmpl := templates[qg.rng.Intn(len(templates))]
		sql := tmpl.Gen(qg.rng)
		queries = append(queries, GeneratedQuery{
			FamilyID:    familyID,
			SQL:         sql,
			QueryType:   tmpl.QueryType,
			TargetRules: tmpl.Rules,
		})
	}
	return queries
}

// queryTempl is the internal query template type.
type queryTempl struct {
	QueryType string
	Rules     []string
	Gen       func(rng *rand.Rand) string
}

// QueryTempl is the exported alias for query templates.
type QueryTempl = queryTempl

// NewQueryTempl creates a query template (exported for dialect packages).
func NewQueryTempl(queryType string, rules []string, gen func(rng *rand.Rand) string) queryTempl {
	return queryTempl{QueryType: queryType, Rules: rules, Gen: gen}
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
			QueryType: "select_star",
			Rules:     []string{"select-star"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s LIMIT 100", t.Name)
			},
		})

		// 2. SELECT specific columns (control)
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "select_columns",
				Rules:     nil,
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "non_sargable",
				Rules:     []string{"non-sargable"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "sargable",
				Rules:     nil,
				Gen: func(rng *rand.Rand) string {
					col := idxCols[rng.Intn(len(idxCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s = %d", t.Name, col, 1+rng.Intn(1000))
				},
			})
		}

		// 5. Unbounded ORDER BY
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "unbounded_sort",
				Rules:     []string{"unbounded-sort"},
				Gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					return fmt.Sprintf("SELECT * FROM %s ORDER BY %s", t.Name, col.Name)
				},
			})
		}

		// 6. Bounded ORDER BY (control)
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "bounded_sort",
				Rules:     nil,
				Gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					limit := 10 + rng.Intn(90)
					return fmt.Sprintf("SELECT * FROM %s ORDER BY %s LIMIT %d", t.Name, col.Name, limit)
				},
			})
		}

		// 7. GROUP BY with aggregation
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "group_by",
				Rules:     []string{"group-by-fanout"},
				Gen: func(rng *rand.Rand) string {
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
			QueryType: "window_no_partition",
			Rules:     []string{"window-function"},
			Gen: func(rng *rand.Rand) string {
				col := cols[rng.Intn(len(cols))]
				return fmt.Sprintf("SELECT %s, row_number() OVER (ORDER BY %s) FROM %s",
					col.Name, col.Name, t.Name)
			},
		})

		// 9. Window function with PARTITION BY
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "window_partitioned",
				Rules:     []string{"window-function"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "exists_subquery",
				Rules:     []string{"correlated-subquery"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "in_subquery",
				Rules:     []string{"correlated-subquery"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "case_expr",
				Rules:     []string{"case-expression"},
				Gen: func(rng *rand.Rand) string {
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
			QueryType: "proper_join",
			Rules:     nil,
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14a. LEFT JOIN (outer join)
		tmpls = append(tmpls, queryTempl{
			QueryType: "left_join",
			Rules:     []string{"outer-join"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s LEFT JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14b. RIGHT JOIN (outer join)
		tmpls = append(tmpls, queryTempl{
			QueryType: "right_join",
			Rules:     []string{"outer-join"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s RIGHT JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14c. FULL JOIN (outer join)
		tmpls = append(tmpls, queryTempl{
			QueryType: "full_join",
			Rules:     []string{"outer-join"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s FULL JOIN %s ON %s.%s = %s.id LIMIT 100",
					jCopy.left, jCopy.right, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})

		// 14. DISTINCT with JOIN (dedup)
		tmpls = append(tmpls, queryTempl{
			QueryType: "distinct_join",
			Rules:     []string{"distinct-dedup"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT DISTINCT %s.* FROM %s JOIN %s ON %s.%s = %s.id",
					jCopy.right, jCopy.right, jCopy.left, jCopy.left, jCopy.leftCol, jCopy.right)
			},
		})
	}

	// 15. Cartesian product (implicit cross join)
	if len(tables) >= 2 {
		tmpls = append(tmpls, queryTempl{
			QueryType: "cartesian",
			Rules:     []string{"cartesian-product", "missing-predicate"},
			Gen: func(rng *rand.Rand) string {
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
			QueryType: "union",
			Rules:     []string{"set-operation"},
			Gen: func(rng *rand.Rand) string {
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
		QueryType: "cte",
		Rules:     []string{"cte"},
		Gen: func(rng *rand.Rand) string {
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
				QueryType: "coalesce_predicate",
				Rules:     []string{"null-coalesce-in-predicate"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "null_check_chain",
				Rules:     []string{"null-check-chain"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "like_leading_wildcard",
				Rules:     []string{"like-leading-wildcard"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "implicit_cast",
				Rules:     []string{"implicit-cast-in-predicate"},
				Gen: func(rng *rand.Rand) string {
					col := intCols[rng.Intn(len(intCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s::text = '%d'",
						t.Name, col.Name, rng.Intn(1000))
				},
			})
		}

		// 18e. Large OFFSET
		tmpls = append(tmpls, queryTempl{
			QueryType: "large_offset",
			Rules:     []string{"large-offset"},
			Gen: func(rng *rand.Rand) string {
				offset := 200 + rng.Intn(800)
				return fmt.Sprintf("SELECT * FROM %s ORDER BY id LIMIT 10 OFFSET %d", t.Name, offset)
			},
		})

		// 18f. Large IN list (25+ values)
		tmpls = append(tmpls, queryTempl{
			QueryType: "large_in_list",
			Rules:     []string{"large-in-list"},
			Gen: func(rng *rand.Rand) string {
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
			QueryType: "for_update",
			Rules:     []string{"for-update-lock"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("SELECT * FROM %s WHERE id = %d FOR UPDATE",
					t.Name, 1+rng.Intn(1000))
			},
		})

		// 18h. DELETE without WHERE (missing-where-clause)
		tmpls = append(tmpls, queryTempl{
			QueryType: "delete_no_where",
			Rules:     []string{"missing-where-clause"},
			Gen: func(rng *rand.Rand) string {
				return fmt.Sprintf("DELETE FROM %s", t.Name)
			},
		})

		// 18i. UPDATE without WHERE (missing-where-clause)
		if len(cols) > 0 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "update_no_where",
				Rules:     []string{"missing-where-clause"},
				Gen: func(rng *rand.Rand) string {
					col := cols[rng.Intn(len(cols))]
					return fmt.Sprintf("UPDATE %s SET %s = %s",
						t.Name, col.Name, sampleValue(col.Type, rng))
				},
			})
		}

		// 18j. INSERT...RETURNING
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "insert_returning",
				Rules:     []string{"returning-clause"},
				Gen: func(rng *rand.Rand) string {
					col := cols[0]
					return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
						t.Name, col.Name, sampleValue(col.Type, rng))
				},
			})
		}

		// 18k. GROUP BY ROLLUP (grouping-sets)
		if len(cols) >= 2 {
			tmpls = append(tmpls, queryTempl{
				QueryType: "grouping_sets",
				Rules:     []string{"grouping-sets"},
				Gen: func(rng *rand.Rand) string {
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
				QueryType: "expensive_func",
				Rules:     []string{"expensive-function"},
				Gen: func(rng *rand.Rand) string {
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
			QueryType: "volatile_func",
			Rules:     []string{"volatile-function"},
			Gen: func(rng *rand.Rand) string {
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
			QueryType: "recursive_cte",
			Rules:     []string{"recursive-cte"},
			Gen: func(rng *rand.Rand) string {
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
			QueryType: "lateral_join",
			Rules:     []string{"lateral-join"},
			Gen: func(rng *rand.Rand) string {
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
			QueryType: "multi_join",
			Rules:     nil, // triggers join + join-count-squared via count
			Gen: func(rng *rand.Rand) string {
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
			QueryType: "union_distinct",
			Rules:     []string{"union-distinct"},
			Gen: func(rng *rand.Rand) string {
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
		QueryType: "ddl_create",
		Rules:     []string{"ddl-statement"},
		Gen: func(rng *rand.Rand) string {
			tmpName := fmt.Sprintf("tmp_cal_%d", rng.Intn(100000))
			return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id serial primary key, val text)", tmpName)
		},
	})

	// 18s. CASCADE DROP
	tmpls = append(tmpls, queryTempl{
		QueryType: "cascade_drop",
		Rules:     []string{"cascade-drop", "ddl-statement"},
		Gen: func(rng *rand.Rand) string {
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
				QueryType: "jsonb_containment",
				Rules:     []string{"expensive-function"},
				Gen: func(rng *rand.Rand) string {
					col := jsonbCols[rng.Intn(len(jsonbCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE %s @> '{\"key\": %d}'",
						t.Name, col.Name, rng.Intn(1000))
				},
			})

			// JSON field extraction in SELECT (deserialization)
			tmpls = append(tmpls, queryTempl{
				QueryType: "jsonb_extract",
				Rules:     []string{"expensive-function"},
				Gen: func(rng *rand.Rand) string {
					col := jsonbCols[rng.Intn(len(jsonbCols))]
					return fmt.Sprintf("SELECT id, %s->>'key' AS key_val, %s->'value' AS val_obj FROM %s WHERE %s IS NOT NULL LIMIT 100",
						col.Name, col.Name, t.Name, col.Name)
				},
			})

			// JSON path query (PG12+)
			tmpls = append(tmpls, queryTempl{
				QueryType: "jsonb_path",
				Rules:     []string{"expensive-function"},
				Gen: func(rng *rand.Rand) string {
					col := jsonbCols[rng.Intn(len(jsonbCols))]
					return fmt.Sprintf("SELECT * FROM %s WHERE jsonb_path_exists(%s, '$.key ? (@ > %d)')",
						t.Name, col.Name, rng.Intn(100))
				},
			})

			// JSON aggregation (building JSON from rows)
			tmpls = append(tmpls, queryTempl{
				QueryType: "jsonb_agg",
				Rules:     []string{"expensive-function"},
				Gen: func(rng *rand.Rand) string {
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
			QueryType: "boolean_nesting",
			Rules:     []string{"boolean-nesting"},
			Gen: func(rng *rand.Rand) string {
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
