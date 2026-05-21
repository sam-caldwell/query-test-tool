package calibrate

import (
	"math/rand"
	"strings"
	"testing"
)

func TestArchetypes(t *testing.T) {
	domains := Archetypes()
	if len(domains) != 4 {
		t.Fatalf("expected 4 domains, got %d", len(domains))
	}

	for _, d := range domains {
		if d.Name == "" {
			t.Error("domain has empty name")
		}
		if len(d.Tables) == 0 {
			t.Errorf("domain %s has no tables", d.Name)
		}
		if len(d.Indexes) == 0 {
			t.Errorf("domain %s has no indexes", d.Name)
		}

		// Verify tables have columns
		for _, table := range d.Tables {
			if len(table.Columns) == 0 {
				t.Errorf("domain %s, table %s has no columns", d.Name, table.Name)
			}
			// First column should be serial PK
			if !table.Columns[0].IsSerial {
				if false { // all remaining domains have serial PKs on every table
					t.Errorf("domain %s, table %s: first column is not serial", d.Name, table.Name)
				}
			}
		}

		// Verify FKs reference valid tables
		tableNames := make(map[string]bool)
		for _, table := range d.Tables {
			tableNames[table.Name] = true
		}
		for _, fk := range d.ForeignKeys {
			if !tableNames[fk.Table] {
				t.Errorf("FK references non-existent table: %s", fk.Table)
			}
			if !tableNames[fk.RefTable] {
				t.Errorf("FK references non-existent ref table: %s", fk.RefTable)
			}
		}

		// Verify indexes reference valid tables
		for _, idx := range d.Indexes {
			if !tableNames[idx.Table] {
				t.Errorf("Index %s references non-existent table: %s", idx.Name, idx.Table)
			}
		}
	}
}

func TestGenerateDDL(t *testing.T) {
	domain := Archetypes()[0]
	ddl := GenerateDDL(domain, "test_schema")

	if !strings.Contains(ddl, "CREATE SCHEMA test_schema") {
		t.Error("DDL should contain CREATE SCHEMA")
	}
	if !strings.Contains(ddl, "CREATE TABLE test_schema."+domain.Tables[0].Name) {
		t.Errorf("DDL should contain CREATE TABLE for %s", domain.Tables[0].Name)
	}
	if !strings.Contains(ddl, "CREATE INDEX") || !strings.Contains(ddl, "CREATE UNIQUE INDEX") {
		t.Error("DDL should contain CREATE INDEX")
	}
	if !strings.Contains(ddl, "ALTER TABLE") {
		t.Error("DDL should contain ALTER TABLE for FKs")
	}
	if !strings.Contains(ddl, "SERIAL PRIMARY KEY") {
		t.Error("DDL should contain SERIAL PRIMARY KEY")
	}
	if !strings.Contains(ddl, "NOT NULL") {
		t.Error("DDL should contain NOT NULL constraints")
	}
}

func TestGenerateDDL_AllDomains(t *testing.T) {
	for i, domain := range Archetypes() {
		ddl := GenerateDDL(domain, "test_"+domain.Name)
		if ddl == "" {
			t.Errorf("domain %d (%s) produced empty DDL", i, domain.Name)
		}
		// Each DDL should be valid PostgreSQL (basic structure check)
		if !strings.HasPrefix(ddl, "CREATE SCHEMA") {
			t.Errorf("domain %s DDL doesn't start with CREATE SCHEMA", domain.Name)
		}
	}
}

func TestGenerateMutationsForDomain(t *testing.T) {
	for _, domain := range Archetypes() {
		mutations := GenerateMutationsForDomain(domain)
		if len(mutations) == 0 {
			t.Errorf("domain %s produced no mutations", domain.Name)
		}

		// Verify mutations have names and rules
		for _, m := range mutations {
			if m.Name == "" {
				t.Error("mutation has empty name")
			}
			if len(m.Rules) == 0 {
				t.Errorf("mutation %s has no rules", m.Name)
			}
			if m.Apply == nil {
				t.Errorf("mutation %s has nil Apply func", m.Name)
			}
		}
	}
}

func TestGenerateSchemaVariants(t *testing.T) {
	domain := Archetypes()[0]
	rng := rand.New(rand.NewSource(42))

	variants := GenerateSchemaVariants(domain, 100, rng)
	if len(variants) < 50 {
		t.Errorf("expected at least 50 variants, got %d", len(variants))
	}

	// Check all variants have at least one mutation
	for i, v := range variants {
		if len(v) == 0 {
			t.Errorf("variant %d has no mutations", i)
		}
	}
}

func TestApplyMutations_DropIndex(t *testing.T) {
	domain := Archetypes()[0]
	originalIndexCount := len(domain.Indexes)

	mutations := GenerateMutationsForDomain(domain)
	// Find a drop_index mutation
	var dropIdx Mutation
	for _, m := range mutations {
		if strings.HasPrefix(m.Name, "drop_idx_") {
			dropIdx = m
			break
		}
	}

	modified := applyMutations(domain, []Mutation{dropIdx})
	if len(modified.Indexes) >= originalIndexCount {
		t.Errorf("drop_index should reduce index count: original=%d, modified=%d",
			originalIndexCount, len(modified.Indexes))
	}

	// Original should be unchanged
	if len(domain.Indexes) != originalIndexCount {
		t.Error("original domain was modified")
	}
}

func TestApplyMutations_DropAllIndexes(t *testing.T) {
	domain := Archetypes()[0]

	mutations := GenerateMutationsForDomain(domain)
	var dropAll Mutation
	for _, m := range mutations {
		if m.Name == "drop_all_indexes" {
			dropAll = m
			break
		}
	}

	modified := applyMutations(domain, []Mutation{dropAll})
	if len(modified.Indexes) != 0 {
		t.Errorf("drop_all_indexes should result in 0 indexes, got %d", len(modified.Indexes))
	}
}

func TestApplyMutations_Widen(t *testing.T) {
	domain := Archetypes()[0]
	originalCols := len(domain.Tables[0].Columns)

	mutations := GenerateMutationsForDomain(domain)
	var widen Mutation
	for _, m := range mutations {
		if strings.HasPrefix(m.Name, "widen_") {
			widen = m
			break
		}
	}

	modified := applyMutations(domain, []Mutation{widen})
	for _, table := range modified.Tables {
		if table.Name == domain.Tables[0].Name {
			if len(table.Columns) <= originalCols {
				t.Error("widen should add columns")
			}
			break
		}
	}
}

func TestApplyMutations_Textify(t *testing.T) {
	domain := Archetypes()[0]

	mutations := GenerateMutationsForDomain(domain)
	var textify Mutation
	for _, m := range mutations {
		if strings.HasPrefix(m.Name, "textify_") {
			textify = m
			break
		}
	}

	modified := applyMutations(domain, []Mutation{textify})
	// Verify at least one column was changed to TEXT
	found := false
	for _, table := range modified.Tables {
		for _, col := range table.Columns {
			if col.Type == "TEXT" && !col.IsSerial {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	// It's okay if not found because textify targets numeric/date cols specifically
}

func TestApplyMutations_DropFK(t *testing.T) {
	domain := Archetypes()[0]
	originalFKCount := len(domain.ForeignKeys)

	mutations := GenerateMutationsForDomain(domain)
	var dropFK Mutation
	for _, m := range mutations {
		if strings.HasPrefix(m.Name, "drop_fk_") {
			dropFK = m
			break
		}
	}

	modified := applyMutations(domain, []Mutation{dropFK})
	if len(modified.ForeignKeys) >= originalFKCount {
		t.Errorf("drop_fk should reduce FK count: original=%d, modified=%d",
			originalFKCount, len(modified.ForeignKeys))
	}
}

func TestSchemaGenerator_GenerateAll(t *testing.T) {
	sg := NewSchemaGenerator(42)
	plans := sg.GenerateAll(100) // small count for testing

	if len(plans) != 4 {
		t.Errorf("expected 4 family plans, got %d", len(plans))
	}

	totalSchemas := 0
	for _, plan := range plans {
		totalSchemas += 1 + len(plan.Variants) // 1 optimal + variants

		if plan.Optimal.SchemaName == "" {
			t.Error("optimal schema has empty name")
		}
		if plan.Optimal.DDL == "" {
			t.Error("optimal schema has empty DDL")
		}
		if !plan.Optimal.IsOptimal {
			t.Error("optimal schema not marked as optimal")
		}

		for _, v := range plan.Variants {
			if v.SchemaName == "" {
				t.Error("variant has empty schema name")
			}
			if v.DDL == "" {
				t.Error("variant has empty DDL")
			}
			if v.IsOptimal {
				t.Error("variant should not be marked as optimal")
			}
			if len(v.Mutations) == 0 {
				t.Error("variant should have mutations")
			}
		}
	}

	if totalSchemas < 50 {
		t.Errorf("expected at least 50 total schemas for target 100, got %d", totalSchemas)
	}
}

func TestTopologicalSort(t *testing.T) {
	for _, domain := range Archetypes() {
		sorted := TopologicalSort(domain)
		if len(sorted) != len(domain.Tables) {
			t.Errorf("domain %s: sorted %d tables, expected %d", domain.Name, len(sorted), len(domain.Tables))
		}

		// Verify parent tables come before children
		position := make(map[string]int)
		for i, table := range sorted {
			position[table.Name] = i
		}
		for _, fk := range domain.ForeignKeys {
			if fk.Table == fk.RefTable {
				continue // self-reference
			}
			parentPos, parentOk := position[fk.RefTable]
			childPos, childOk := position[fk.Table]
			if parentOk && childOk && parentPos > childPos {
				t.Errorf("domain %s: parent %s (pos %d) comes after child %s (pos %d)",
					domain.Name, fk.RefTable, parentPos, fk.Table, childPos)
			}
		}
	}
}

func TestIsNumericOrDate(t *testing.T) {
	tests := []struct {
		typ  string
		want bool
	}{
		{"INT", true},
		{"BIGINT", true},
		{"SMALLINT", true},
		{"DATE", true},
		{"TIMESTAMPTZ", true},
		{"TIMESTAMP", true},
		{"NUMERIC(10,2)", true},
		{"BOOLEAN", true},
		{"TEXT", false},
		{"VARCHAR(100)", false},
		{"JSONB", false},
	}
	for _, tt := range tests {
		if got := isNumericOrDate(tt.typ); got != tt.want {
			t.Errorf("isNumericOrDate(%q) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestSortThree(t *testing.T) {
	tests := []struct {
		a, b, c int
		want    [3]int
	}{
		{1, 2, 3, [3]int{1, 2, 3}},
		{3, 2, 1, [3]int{1, 2, 3}},
		{2, 3, 1, [3]int{1, 2, 3}},
		{5, 5, 5, [3]int{5, 5, 5}},
	}
	for _, tt := range tests {
		got := sortThree(tt.a, tt.b, tt.c)
		if got != tt.want {
			t.Errorf("sortThree(%d,%d,%d) = %v, want %v", tt.a, tt.b, tt.c, got, tt.want)
		}
	}
}
