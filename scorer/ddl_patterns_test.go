package scorer

import (
	"testing"

	"github.com/sam-caldwell/query-test-tool/parser"
)

func TestDDLStatement(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"CREATE TABLE triggers",
			"CREATE TABLE users (id serial PRIMARY KEY, name text NOT NULL)",
			true,
		},
		{
			"CREATE INDEX triggers",
			"CREATE INDEX idx_users_name ON users (name)",
			true,
		},
		{
			"ALTER TABLE triggers",
			"ALTER TABLE users ADD COLUMN email text",
			true,
		},
		{
			"CREATE VIEW triggers",
			"CREATE VIEW active_users AS SELECT * FROM users WHERE active = true",
			true,
		},
		{
			"CREATE FUNCTION triggers",
			"CREATE FUNCTION add(a int, b int) RETURNS int AS $$ SELECT a + b; $$ LANGUAGE SQL",
			true,
		},
		{
			"DROP TABLE triggers",
			"DROP TABLE users",
			true,
		},
		{
			"CREATE TRIGGER triggers",
			"CREATE TRIGGER update_timestamp BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION update_modified()",
			true,
		},
		{
			"SELECT does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
		{
			"INSERT does not trigger",
			"INSERT INTO users (name) VALUES ('test')",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDDLPatterns(tree, &cog)
			hit := hasRule(cog.Findings, "ddl-statement")
			if hit != tt.wantHit {
				t.Errorf("ddl-statement: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, cog.Findings)
			}
		})
	}
}

func TestCascadeDrop(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{
			"DROP CASCADE triggers",
			"DROP TABLE users CASCADE",
			true,
		},
		{
			"DROP RESTRICT does not trigger",
			"DROP TABLE users RESTRICT",
			false,
		},
		{
			"DROP without behavior does not trigger",
			"DROP TABLE users",
			false,
		},
		{
			"DROP SCHEMA CASCADE triggers",
			"DROP SCHEMA public CASCADE",
			true,
		},
		{
			"SELECT does not trigger",
			"SELECT * FROM users WHERE id = 1",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parser.Parse(tt.sql)
			if err != nil {
				t.Fatalf("failed to parse SQL: %v", err)
			}
			cog := DimensionScore{Name: "cognitive_complexity"}
			scoreDDLPatterns(tree, &cog)
			hit := hasRule(cog.Findings, "cascade-drop")
			if hit != tt.wantHit {
				t.Errorf("cascade-drop: got hit=%v, want %v (findings=%v)",
					hit, tt.wantHit, cog.Findings)
			}
		})
	}
}

func TestDDLPatterns_NilHandling(t *testing.T) {
	cog := DimensionScore{Name: "cognitive_complexity"}
	scoreDDLNode(nil, &cog)
	if cog.Score != 0 {
		t.Error("nil node should produce no findings")
	}
}

func TestDDLPatterns_CleanQuery(t *testing.T) {
	sql := "SELECT id, name FROM users WHERE id = 1"
	tree, err := parser.Parse(sql)
	if err != nil {
		t.Fatal(err)
	}
	cog := DimensionScore{Name: "cognitive_complexity"}
	scoreDDLPatterns(tree, &cog)
	if cog.Score != 0 {
		t.Errorf("clean query should have score 0, got %d", cog.Score)
	}
	if len(cog.Findings) != 0 {
		t.Errorf("clean query should have no findings, got %v", cog.Findings)
	}
}
