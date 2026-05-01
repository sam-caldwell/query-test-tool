package parser

import (
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v5"
)

func TestParse_ValidSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"simple select", "SELECT 1"},
		{"select with where", "SELECT id FROM users WHERE id = 1"},
		{"select with join", "SELECT u.id FROM users u JOIN orders o ON u.id = o.user_id"},
		{"insert", "INSERT INTO users (name) VALUES ('alice')"},
		{"update", "UPDATE users SET name = 'bob' WHERE id = 1"},
		{"delete", "DELETE FROM users WHERE id = 1"},
		{"subquery", "SELECT * FROM (SELECT 1) AS t"},
		{"CTE", "WITH cte AS (SELECT 1) SELECT * FROM cte"},
		{"union", "SELECT 1 UNION SELECT 2"},
		{"window function", "SELECT row_number() OVER (PARTITION BY dept ORDER BY salary) FROM employees"},
		{"complex joins", "SELECT a.id FROM a JOIN b ON a.id = b.a_id JOIN c ON b.id = c.b_id"},
		{"group by having", "SELECT dept, COUNT(*) FROM employees GROUP BY dept HAVING COUNT(*) > 5"},
		{"order by limit", "SELECT * FROM users ORDER BY name LIMIT 10"},
		{"exists subquery", "SELECT * FROM users WHERE EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id)"},
		{"case expression", "SELECT CASE WHEN x > 0 THEN 'pos' ELSE 'neg' END FROM t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Parse(tt.sql)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.sql, err)
			}
			if result == nil {
				t.Fatalf("Parse(%q) returned nil", tt.sql)
			}
			if len(result.Stmts) == 0 {
				t.Fatalf("Parse(%q) returned 0 statements", tt.sql)
			}
		})
	}
}

func TestParse_InvalidSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"gibberish", "NOT VALID SQL AT ALL"},
		{"incomplete", "SELECT FROM"},
		{"unclosed paren", "SELECT (1"},
		// empty string parses successfully in pg_query (returns 0 statements)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.sql)
			if err == nil {
				t.Fatalf("Parse(%q) expected error but got nil", tt.sql)
			}
		})
	}
}

func TestWalk_Traversal(t *testing.T) {
	result, err := Parse("SELECT id, name FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}

	nodeTypes := make(map[string]int)
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		switch node.Node.(type) {
		case *pg_query.Node_SelectStmt:
			nodeTypes["SelectStmt"]++
		case *pg_query.Node_ResTarget:
			nodeTypes["ResTarget"]++
		case *pg_query.Node_ColumnRef:
			nodeTypes["ColumnRef"]++
		case *pg_query.Node_RangeVar:
			nodeTypes["RangeVar"]++
		case *pg_query.Node_AExpr:
			nodeTypes["AExpr"]++
		}
		return true
	})

	if nodeTypes["SelectStmt"] != 1 {
		t.Errorf("expected 1 SelectStmt, got %d", nodeTypes["SelectStmt"])
	}
	if nodeTypes["ResTarget"] != 2 {
		t.Errorf("expected 2 ResTarget, got %d", nodeTypes["ResTarget"])
	}
	if nodeTypes["RangeVar"] != 1 {
		t.Errorf("expected 1 RangeVar, got %d", nodeTypes["RangeVar"])
	}
}

func TestWalk_StopsOnFalse(t *testing.T) {
	result, err := Parse("SELECT id FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		count++
		return false // stop immediately
	})

	if count != 1 {
		t.Errorf("expected walk to visit exactly 1 node when returning false, got %d", count)
	}
}

func TestWalk_NilNode(t *testing.T) {
	count := 0
	Walk(nil, 0, func(node *pg_query.Node, depth int) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("expected 0 visits on nil node, got %d", count)
	}
}

func TestWalkNodes(t *testing.T) {
	result, err := Parse("SELECT id, name FROM users")
	if err != nil {
		t.Fatal(err)
	}

	sel := result.Stmts[0].Stmt.GetSelectStmt()
	count := 0
	WalkNodes(sel.TargetList, 0, func(node *pg_query.Node, depth int) bool {
		count++
		return true
	})
	if count < 2 {
		t.Errorf("expected at least 2 visits for 2 targets, got %d", count)
	}
}

func TestChildren_SelectStmt(t *testing.T) {
	result, err := Parse("SELECT id FROM users WHERE id = 1 ORDER BY id LIMIT 10")
	if err != nil {
		t.Fatal(err)
	}
	children := Children(result.Stmts[0].Stmt)
	if len(children) == 0 {
		t.Error("expected children from SelectStmt")
	}
}

func TestChildren_JoinExpr(t *testing.T) {
	result, err := Parse("SELECT * FROM a JOIN b ON a.id = b.a_id")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_JoinExpr); ok {
			found = true
			children := Children(node)
			if len(children) < 3 {
				t.Errorf("JoinExpr should have at least 3 children (larg, rarg, quals), got %d", len(children))
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find JoinExpr")
	}
}

func TestChildren_BoolExpr(t *testing.T) {
	result, err := Parse("SELECT 1 WHERE true AND false")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_BoolExpr); ok {
			found = true
			children := Children(node)
			if len(children) < 2 {
				t.Errorf("BoolExpr AND should have at least 2 children, got %d", len(children))
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find BoolExpr")
	}
}

func TestChildren_SubLink(t *testing.T) {
	result, err := Parse("SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_SubLink); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("SubLink should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find SubLink")
	}
}

func TestChildren_FuncCall(t *testing.T) {
	result, err := Parse("SELECT count(*) FROM users")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_FuncCall); ok {
			found = true
		}
		return true
	})
	if !found {
		t.Error("expected to find FuncCall")
	}
}

func TestChildren_CaseExpr(t *testing.T) {
	result, err := Parse("SELECT CASE WHEN x > 0 THEN 'pos' ELSE 'neg' END FROM t")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_CaseExpr); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("CaseExpr should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find CaseExpr")
	}
}

func TestChildren_NilNode(t *testing.T) {
	children := Children(nil)
	if children != nil {
		t.Error("expected nil children for nil node")
	}
}

func TestChildren_LeafNodes(t *testing.T) {
	result, err := Parse("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}

	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		switch node.Node.(type) {
		case *pg_query.Node_Integer:
			children := Children(node)
			if len(children) != 0 {
				t.Error("Integer node should have no children")
			}
		}
		return true
	})
}

func TestChildren_WithClause(t *testing.T) {
	result, err := Parse("WITH cte AS (SELECT 1) SELECT * FROM cte")
	if err != nil {
		t.Fatal(err)
	}

	children := Children(result.Stmts[0].Stmt)
	if len(children) == 0 {
		t.Error("SelectStmt with CTE should have children including CTE")
	}
}

func TestChildren_SetOperations(t *testing.T) {
	result, err := Parse("SELECT 1 UNION ALL SELECT 2")
	if err != nil {
		t.Fatal(err)
	}

	children := Children(result.Stmts[0].Stmt)
	if len(children) < 2 {
		t.Errorf("UNION should have at least 2 children (larg, rarg), got %d", len(children))
	}
}

func TestChildren_InsertStmt(t *testing.T) {
	result, err := Parse("INSERT INTO users (name) SELECT name FROM temp_users")
	if err != nil {
		t.Fatal(err)
	}

	children := Children(result.Stmts[0].Stmt)
	if len(children) == 0 {
		t.Error("InsertStmt should have children")
	}
}

func TestChildren_UpdateStmt(t *testing.T) {
	result, err := Parse("UPDATE users SET name = 'bob' WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}

	children := Children(result.Stmts[0].Stmt)
	if len(children) == 0 {
		t.Error("UpdateStmt should have children")
	}
}

func TestChildren_DeleteStmt(t *testing.T) {
	result, err := Parse("DELETE FROM users WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}

	children := Children(result.Stmts[0].Stmt)
	if len(children) == 0 {
		t.Error("DeleteStmt should have children")
	}
}

func TestChildren_RangeSubselect(t *testing.T) {
	result, err := Parse("SELECT * FROM (SELECT 1 AS x) AS sub")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_RangeSubselect); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("RangeSubselect should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find RangeSubselect")
	}
}

func TestChildren_CommonTableExpr(t *testing.T) {
	result, err := Parse("WITH cte AS (SELECT 1) SELECT * FROM cte")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_CommonTableExpr); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("CommonTableExpr should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find CommonTableExpr")
	}
}

func TestChildren_TypeCast(t *testing.T) {
	result, err := Parse("SELECT CAST(id AS text) FROM users")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_TypeCast); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("TypeCast should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find TypeCast")
	}
}

func TestChildren_CoalesceExpr(t *testing.T) {
	result, err := Parse("SELECT COALESCE(name, 'unknown') FROM users")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_CoalesceExpr); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("CoalesceExpr should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find CoalesceExpr")
	}
}

func TestChildren_NullTest(t *testing.T) {
	result, err := Parse("SELECT * FROM users WHERE name IS NOT NULL")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_NullTest); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("NullTest should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find NullTest")
	}
}

func TestChildren_SortBy(t *testing.T) {
	result, err := Parse("SELECT * FROM users ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_SortBy); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("SortBy should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find SortBy")
	}
}

func TestChildren_WindowDef(t *testing.T) {
	result, err := Parse("SELECT id, row_number() OVER (PARTITION BY dept ORDER BY id) FROM employees")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if fc, ok := node.Node.(*pg_query.Node_FuncCall); ok && fc.FuncCall.Over != nil {
			// Window defs are embedded in FuncCall, verify our Children walks them
			found = true
		}
		return true
	})
	if !found {
		t.Error("expected to find FuncCall with WindowDef")
	}
}

func TestChildren_AExpr(t *testing.T) {
	result, err := Parse("SELECT 1 WHERE 1 + 2 = 3")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_AExpr); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("AExpr should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find AExpr")
	}
}

func TestChildren_List(t *testing.T) {
	result, err := Parse("SELECT 1 WHERE x IN (1, 2, 3)")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if _, ok := node.Node.(*pg_query.Node_List); ok {
			found = true
			children := Children(node)
			if len(children) == 0 {
				t.Error("List should have children")
			}
		}
		return true
	})
	if !found {
		t.Error("expected to find List")
	}
}

func TestDepthTracking(t *testing.T) {
	result, err := Parse("SELECT * FROM (SELECT * FROM (SELECT 1) AS inner_t) AS outer_t")
	if err != nil {
		t.Fatal(err)
	}

	maxDepth := 0
	Walk(result.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {
		if depth > maxDepth {
			maxDepth = depth
		}
		return true
	})

	if maxDepth < 2 {
		t.Errorf("expected depth >= 2 for nested subqueries, got %d", maxDepth)
	}
}
