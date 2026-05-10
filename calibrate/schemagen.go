package calibrate

import (
	"fmt"
	"math/rand"
	"strings"
)

// SchemaGenerator creates schema families with optimal and degraded variants.
type SchemaGenerator struct {
	rng *rand.Rand
}

// NewSchemaGenerator creates a new schema generator with the given seed.
func NewSchemaGenerator(seed int64) *SchemaGenerator {
	return &SchemaGenerator{rng: rand.New(rand.NewSource(seed))}
}

// GenerateAll produces schema families targeting approximately targetSchemas total instances.
func (sg *SchemaGenerator) GenerateAll(targetSchemas int) []SchemaFamilyPlan {
	domains := Archetypes()
	schemasPerDomain := targetSchemas / len(domains)

	var families []SchemaFamilyPlan
	schemaCounter := 0

	for _, domain := range domains {
		variants := GenerateSchemaVariants(domain, schemasPerDomain-1, sg.rng)

		familyName := fmt.Sprintf("%s_family", domain.Name)

		plan := SchemaFamilyPlan{
			Domain:      domain,
			FamilyName:  familyName,
			Description: domain.Description,
		}

		// Optimal schema
		schemaCounter++
		optDDL := GenerateDDL(domain, fmt.Sprintf("cal_%05d", schemaCounter))
		plan.Optimal = SchemaInstance{
			SchemaName: fmt.Sprintf("cal_%05d", schemaCounter),
			IsOptimal:  true,
			DDL:        optDDL,
		}

		// Degraded variants
		for _, mutationSet := range variants {
			schemaCounter++
			degraded := applyMutations(domain, mutationSet)
			schemaName := fmt.Sprintf("cal_%05d", schemaCounter)
			ddl := GenerateDDL(degraded, schemaName)

			var mutNames []string
			for _, m := range mutationSet {
				mutNames = append(mutNames, m.Name)
			}

			plan.Variants = append(plan.Variants, SchemaInstance{
				SchemaName: schemaName,
				IsOptimal:  false,
				Mutations:  mutNames,
				DDL:        ddl,
			})
		}

		families = append(families, plan)
	}

	return families
}

// SchemaFamilyPlan holds a complete family before insertion.
type SchemaFamilyPlan struct {
	Domain      Domain
	FamilyName  string
	Description string
	Optimal     SchemaInstance
	Variants    []SchemaInstance
}

// applyMutations creates a deep copy of the domain and applies all mutations.
func applyMutations(d Domain, mutations []Mutation) Domain {
	// Deep copy
	copy := Domain{
		Name:        d.Name,
		Description: d.Description,
	}

	// Copy tables
	for _, t := range d.Tables {
		newTable := TableDef{Name: t.Name}
		newTable.Columns = append([]ColumnDef{}, t.Columns...)
		copy.Tables = append(copy.Tables, newTable)
	}
	copy.Indexes = append([]IndexDef{}, d.Indexes...)
	copy.ForeignKeys = append([]FKDef{}, d.ForeignKeys...)

	// Apply mutations
	for _, m := range mutations {
		m.Apply(&copy)
	}
	return copy
}

// GenerateDDL produces the full DDL for a domain within a schema.
func GenerateDDL(d Domain, schemaName string) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("CREATE SCHEMA %s;\n\n", schemaName))

	// Tables
	for _, table := range d.Tables {
		b.WriteString(generateCreateTable(schemaName, table))
		b.WriteString("\n")
	}

	// Indexes
	for _, idx := range d.Indexes {
		b.WriteString(generateCreateIndex(schemaName, idx))
		b.WriteString("\n")
	}

	// Foreign keys
	for _, fk := range d.ForeignKeys {
		b.WriteString(generateAlterAddFK(schemaName, fk))
		b.WriteString("\n")
	}

	return b.String()
}

// GenerateDDLTablesOnly generates DDL with UNLOGGED tables and no indexes or
// foreign keys. Used during batch-and-drop calibration for fast bulk inserts.
func GenerateDDLTablesOnly(d Domain, schemaName string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE SCHEMA %s;\n\n", schemaName))
	for _, table := range d.Tables {
		b.WriteString(generateCreateUnloggedTable(schemaName, table))
		b.WriteString("\n")
	}
	return b.String()
}

// GenerateDDLIndexesAndFKs generates DDL for indexes and foreign keys only.
// Applied after data population to avoid index maintenance during bulk inserts.
func GenerateDDLIndexesAndFKs(d Domain, schemaName string) string {
	var b strings.Builder
	for _, idx := range d.Indexes {
		b.WriteString(generateCreateIndex(schemaName, idx))
		b.WriteString("\n")
	}
	for _, fk := range d.ForeignKeys {
		b.WriteString(generateAlterAddFK(schemaName, fk))
		b.WriteString("\n")
	}
	return b.String()
}

func generateCreateUnloggedTable(schema string, t TableDef) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE UNLOGGED TABLE %s.%s (\n", schema, t.Name))

	for i, col := range t.Columns {
		b.WriteString("  ")
		if col.IsSerial {
			if col.Type == "BIGSERIAL" {
				b.WriteString(fmt.Sprintf("%s BIGSERIAL PRIMARY KEY", col.Name))
			} else {
				b.WriteString(fmt.Sprintf("%s SERIAL PRIMARY KEY", col.Name))
			}
		} else {
			b.WriteString(fmt.Sprintf("%s %s", col.Name, col.Type))
			if col.NotNull {
				b.WriteString(" NOT NULL")
			}
			if col.Default != "" {
				b.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
			}
		}
		if i < len(t.Columns)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(");\n")
	return b.String()
}

func generateCreateTable(schema string, t TableDef) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE %s.%s (\n", schema, t.Name))

	for i, col := range t.Columns {
		b.WriteString("  ")
		if col.IsSerial {
			if col.Type == "BIGSERIAL" {
				b.WriteString(fmt.Sprintf("%s BIGSERIAL PRIMARY KEY", col.Name))
			} else {
				b.WriteString(fmt.Sprintf("%s SERIAL PRIMARY KEY", col.Name))
			}
		} else {
			b.WriteString(fmt.Sprintf("%s %s", col.Name, col.Type))
			if col.NotNull {
				b.WriteString(" NOT NULL")
			}
			if col.Default != "" {
				b.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
			}
		}
		if i < len(t.Columns)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(");\n")
	return b.String()
}

func generateCreateIndex(schema string, idx IndexDef) string {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	if idx.Expression != "" {
		return fmt.Sprintf("CREATE %sINDEX %s ON %s.%s (%s);\n",
			unique, idx.Name, schema, idx.Table, idx.Expression)
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s.%s (%s);\n",
		unique, idx.Name, schema, idx.Table, strings.Join(idx.Columns, ", "))
}

func generateAlterAddFK(schema string, fk FKDef) string {
	return fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s(%s);\n",
		schema, fk.Table, fk.Name, fk.Column, schema, fk.RefTable, fk.RefColumn)
}
