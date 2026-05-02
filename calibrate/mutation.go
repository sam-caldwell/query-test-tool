package calibrate

import (
	"fmt"
	"math/rand"
)

// MutationDef defines a reusable mutation generator.
type MutationDef struct {
	Name        string
	Rules       []string
	Description string
	// Generate returns concrete mutations for a domain.
	Generate func(d Domain) []Mutation
}

// AllMutationDefs returns all defined mutation generators.
func AllMutationDefs() []MutationDef {
	return []MutationDef{
		dropSingleIndex(),
		dropAllIndexes(),
		dropForeignKey(),
		widenTable(),
		textifyColumn(),
		removeNotNull(),
		denormalizeTable(),
		addRedundantColumns(),
	}
}

// GenerateMutationsForDomain generates all applicable mutations for a domain.
func GenerateMutationsForDomain(d Domain) []Mutation {
	var all []Mutation
	for _, def := range AllMutationDefs() {
		all = append(all, def.Generate(d)...)
	}
	return all
}

// GenerateSchemaVariants generates schema variants by applying single and combined mutations.
// It targets approximately targetCount variants per domain.
func GenerateSchemaVariants(d Domain, targetCount int, rng *rand.Rand) [][]Mutation {
	mutations := GenerateMutationsForDomain(d)
	if len(mutations) == 0 {
		return nil
	}

	var variants [][]Mutation

	// All single mutations
	for i := range mutations {
		variants = append(variants, []Mutation{mutations[i]})
	}

	// Double mutations — sample to hit target
	doublesNeeded := targetCount - len(mutations)
	if doublesNeeded < 0 {
		doublesNeeded = 0
	}
	maxDoubles := len(mutations) * (len(mutations) - 1) / 2
	if doublesNeeded > maxDoubles {
		doublesNeeded = maxDoubles
	}

	doublesSeen := make(map[string]bool)
	for len(doublesSeen) < doublesNeeded {
		i := rng.Intn(len(mutations))
		j := rng.Intn(len(mutations))
		if i == j {
			continue
		}
		if i > j {
			i, j = j, i
		}
		key := fmt.Sprintf("%d:%d", i, j)
		if doublesSeen[key] {
			continue
		}
		doublesSeen[key] = true
		variants = append(variants, []Mutation{mutations[i], mutations[j]})
	}

	// Triples — fill remainder if needed
	triplesNeeded := targetCount - len(variants)
	if triplesNeeded > 0 && len(mutations) >= 3 {
		triplesSeen := make(map[string]bool)
		attempts := 0
		for len(triplesSeen) < triplesNeeded && attempts < triplesNeeded*10 {
			attempts++
			i := rng.Intn(len(mutations))
			j := rng.Intn(len(mutations))
			k := rng.Intn(len(mutations))
			if i == j || j == k || i == k {
				continue
			}
			idxs := sortThree(i, j, k)
			key := fmt.Sprintf("%d:%d:%d", idxs[0], idxs[1], idxs[2])
			if triplesSeen[key] {
				continue
			}
			triplesSeen[key] = true
			variants = append(variants, []Mutation{mutations[idxs[0]], mutations[idxs[1]], mutations[idxs[2]]})
		}
	}

	return variants
}

func sortThree(a, b, c int) [3]int {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b, c = c, b
	}
	if a > b {
		a, b = b, a
	}
	return [3]int{a, b, c}
}

// --- Mutation Generators ---

func dropSingleIndex() MutationDef {
	return MutationDef{
		Name:        "drop_index",
		Rules:       []string{"non-sargable", "unbounded-sort"},
		Description: "Remove a single index to test index-dependent query patterns",
		Generate: func(d Domain) []Mutation {
			var muts []Mutation
			for _, idx := range d.Indexes {
				idxCopy := idx
				muts = append(muts, Mutation{
					Name:        fmt.Sprintf("drop_idx_%s_%s", idx.Table, idx.Name),
					Description: fmt.Sprintf("Drop index %s on %s", idx.Name, idx.Table),
					Rules:       []string{"non-sargable", "unbounded-sort"},
					Apply: func(d *Domain) {
						var remaining []IndexDef
						for _, existing := range d.Indexes {
							if existing.Name != idxCopy.Name {
								remaining = append(remaining, existing)
							}
						}
						d.Indexes = remaining
					},
				})
			}
			return muts
		},
	}
}

func dropAllIndexes() MutationDef {
	return MutationDef{
		Name:        "drop_all_indexes",
		Rules:       []string{"non-sargable", "unbounded-sort"},
		Description: "Remove all non-PK indexes",
		Generate: func(d Domain) []Mutation {
			return []Mutation{{
				Name:        "drop_all_indexes",
				Description: "Remove all non-primary-key indexes",
				Rules:       []string{"non-sargable", "unbounded-sort"},
				Apply: func(d *Domain) {
					d.Indexes = nil
				},
			}}
		},
	}
}

func dropForeignKey() MutationDef {
	return MutationDef{
		Name:        "drop_fk",
		Rules:       []string{"missing-predicate"},
		Description: "Remove foreign key constraints",
		Generate: func(d Domain) []Mutation {
			var muts []Mutation
			for _, fk := range d.ForeignKeys {
				fkCopy := fk
				muts = append(muts, Mutation{
					Name:        fmt.Sprintf("drop_fk_%s_%s", fk.Table, fk.Column),
					Description: fmt.Sprintf("Drop FK %s.%s -> %s.%s", fk.Table, fk.Column, fk.RefTable, fk.RefColumn),
					Rules:       []string{"missing-predicate"},
					Apply: func(d *Domain) {
						var remaining []FKDef
						for _, existing := range d.ForeignKeys {
							if existing.Name != fkCopy.Name {
								remaining = append(remaining, existing)
							}
						}
						d.ForeignKeys = remaining
					},
				})
			}
			return muts
		},
	}
}

func widenTable() MutationDef {
	return MutationDef{
		Name:        "widen_table",
		Rules:       []string{"select-star"},
		Description: "Add extra unused columns to increase SELECT * cost",
		Generate: func(d Domain) []Mutation {
			var muts []Mutation
			for _, table := range d.Tables {
				for _, extraCols := range []int{10, 20, 50} {
					tableCopy := table.Name
					n := extraCols
					muts = append(muts, Mutation{
						Name:        fmt.Sprintf("widen_%s_%d", tableCopy, n),
						Description: fmt.Sprintf("Add %d extra columns to %s", n, tableCopy),
						Rules:       []string{"select-star"},
						Apply: func(d *Domain) {
							for i := range d.Tables {
								if d.Tables[i].Name == tableCopy {
									baseOffset := len(d.Tables[i].Columns)
									for j := 0; j < n; j++ {
										d.Tables[i].Columns = append(d.Tables[i].Columns, ColumnDef{
											Name: fmt.Sprintf("extra_col_%d_%d", baseOffset, j),
											Type: "TEXT",
										})
									}
									break
								}
							}
						},
					})
				}
			}
			return muts
		},
	}
}

func textifyColumn() MutationDef {
	return MutationDef{
		Name:        "textify",
		Rules:       []string{"non-sargable"},
		Description: "Change numeric/date columns to TEXT to force type coercion",
		Generate: func(d Domain) []Mutation {
			// Build set of FK columns to skip (textifying breaks FK type constraints)
			fkCols := make(map[string]bool)
			for _, fk := range d.ForeignKeys {
				fkCols[fk.Table+"."+fk.Column] = true
			}

			var muts []Mutation
			for _, table := range d.Tables {
				for _, col := range table.Columns {
					if col.IsSerial || col.Type == "TEXT" || col.Type == "JSONB" {
						continue
					}
					if fkCols[table.Name+"."+col.Name] {
						continue // skip FK columns
					}
					// Only textify numeric and date types
					if isNumericOrDate(col.Type) {
						tableCopy := table.Name
						colCopy := col.Name
						muts = append(muts, Mutation{
							Name:        fmt.Sprintf("textify_%s_%s", tableCopy, colCopy),
							Description: fmt.Sprintf("Change %s.%s to TEXT", tableCopy, colCopy),
							Rules:       []string{"non-sargable"},
							Apply: func(d *Domain) {
								for i := range d.Tables {
									if d.Tables[i].Name == tableCopy {
										for j := range d.Tables[i].Columns {
											if d.Tables[i].Columns[j].Name == colCopy {
												d.Tables[i].Columns[j].Type = "TEXT"
												d.Tables[i].Columns[j].Default = ""
												break
											}
										}
										break
									}
								}
							},
						})
					}
				}
			}
			return muts
		},
	}
}

func removeNotNull() MutationDef {
	return MutationDef{
		Name:        "remove_not_null",
		Rules:       []string{"non-sargable"},
		Description: "Remove NOT NULL constraints to introduce nullability",
		Generate: func(d Domain) []Mutation {
			var muts []Mutation
			for _, table := range d.Tables {
				tableCopy := table.Name
				muts = append(muts, Mutation{
					Name:        fmt.Sprintf("nullable_%s", tableCopy),
					Description: fmt.Sprintf("Remove all NOT NULL constraints from %s", tableCopy),
					Rules:       []string{"non-sargable"},
					Apply: func(d *Domain) {
						for i := range d.Tables {
							if d.Tables[i].Name == tableCopy {
								for j := range d.Tables[i].Columns {
									if !d.Tables[i].Columns[j].IsSerial {
										d.Tables[i].Columns[j].NotNull = false
									}
								}
								break
							}
						}
					},
				})
			}
			return muts
		},
	}
}

// denormalizeTable merges a child table's columns into the parent table,
// simulating denormalization. This creates wider tables with redundant data.
func denormalizeTable() MutationDef {
	return MutationDef{
		Name:        "denormalize",
		Rules:       []string{"select-star", "non-sargable"},
		Description: "Merge child table columns into parent (denormalization)",
		Generate: func(d Domain) []Mutation {
			var muts []Mutation
			for _, fk := range d.ForeignKeys {
				fkCopy := fk
				// Find the child table and parent table
				var childTable, parentTable *TableDef
				for i := range d.Tables {
					if d.Tables[i].Name == fkCopy.Table {
						childTable = &d.Tables[i]
					}
					if d.Tables[i].Name == fkCopy.RefTable {
						parentTable = &d.Tables[i]
					}
				}
				if childTable == nil || parentTable == nil {
					continue
				}
				if childTable.Name == parentTable.Name {
					continue // skip self-references
				}

				muts = append(muts, Mutation{
					Name:        fmt.Sprintf("denorm_%s_into_%s", fkCopy.Table, fkCopy.RefTable),
					Description: fmt.Sprintf("Flatten %s columns into %s (denormalize)", fkCopy.Table, fkCopy.RefTable),
					Rules:       []string{"select-star", "non-sargable"},
					Apply: func(d *Domain) {
						var srcCols []ColumnDef
						for _, t := range d.Tables {
							if t.Name == fkCopy.Table {
								for _, col := range t.Columns {
									if !col.IsSerial && col.Name != fkCopy.Column {
										srcCols = append(srcCols, col)
									}
								}
								break
							}
						}
						// Add child columns to parent with prefix
						for i := range d.Tables {
							if d.Tables[i].Name == fkCopy.RefTable {
								for _, col := range srcCols {
									d.Tables[i].Columns = append(d.Tables[i].Columns, ColumnDef{
										Name:    fkCopy.Table + "_" + col.Name,
										Type:    col.Type,
										NotNull: false, // denormalized columns are nullable
									})
								}
								break
							}
						}
					},
				})
			}
			return muts
		},
	}
}

// addRedundantColumns adds computed/cached columns that duplicate existing data,
// simulating a partially denormalized schema with redundant storage.
func addRedundantColumns() MutationDef {
	return MutationDef{
		Name:        "redundant_cols",
		Rules:       []string{"select-star"},
		Description: "Add redundant computed columns (simulates cached denormalization)",
		Generate: func(d Domain) []Mutation {
			var muts []Mutation
			for _, table := range d.Tables {
				tableCopy := table.Name
				muts = append(muts, Mutation{
					Name:        fmt.Sprintf("redundant_%s", tableCopy),
					Description: fmt.Sprintf("Add redundant cached columns to %s", tableCopy),
					Rules:       []string{"select-star"},
					Apply: func(d *Domain) {
						for i := range d.Tables {
							if d.Tables[i].Name == tableCopy {
								// Add typical denormalization columns
								d.Tables[i].Columns = append(d.Tables[i].Columns,
									ColumnDef{Name: "cached_count", Type: "INT"},
									ColumnDef{Name: "cached_total", Type: "NUMERIC(12,2)"},
									ColumnDef{Name: "cached_label", Type: "TEXT"},
									ColumnDef{Name: "last_computed_at", Type: "TIMESTAMPTZ"},
									ColumnDef{Name: "denorm_status", Type: "VARCHAR(50)"},
								)
								break
							}
						}
					},
				})
			}
			return muts
		},
	}
}

func isNumericOrDate(t string) bool {
	switch {
	case t == "INT", t == "BIGINT", t == "SMALLINT":
		return true
	case t == "DATE", t == "TIMESTAMPTZ", t == "TIMESTAMP":
		return true
	case len(t) >= 7 && t[:7] == "NUMERIC":
		return true
	case t == "BOOLEAN":
		return true
	}
	return false
}
