package scorer

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/sam-caldwell/query-test-tool/src/parser"
)

// DDL pattern penalty accessors — values loaded from embedded weights.json,
// falling back to zero until these rules are added to the weights file.
func PenaltyDDLStatement() int { return Weight("ddl-statement") }
func PenaltyCascadeDrop() int  { return Weight("cascade-drop") }

// scoreDDLPatterns detects DDL statements and flags high-risk operations.
func scoreDDLPatterns(tree *pg_query.ParseResult, cog *DimensionScore) {
	for _, stmt := range tree.Stmts {
		scoreDDLNode(stmt.Stmt, cog)
	}
}

func scoreDDLNode(node *pg_query.Node, cog *DimensionScore) {
	if node == nil {
		return
	}

	parser.Walk(node, 0, func(n *pg_query.Node, depth int) bool {
		switch v := n.Node.(type) {
		case *pg_query.Node_CreateStmt:
			_ = v
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "CREATE TABLE is a DDL operation with schema-level impact",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		case *pg_query.Node_IndexStmt:
			_ = v
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "CREATE INDEX is a DDL operation — may lock the table",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		case *pg_query.Node_AlterTableStmt:
			_ = v
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "ALTER TABLE is a DDL operation — may require table rewrite or lock",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		case *pg_query.Node_ViewStmt:
			_ = v
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "CREATE VIEW is a DDL operation defining a virtual table",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		case *pg_query.Node_CreateFunctionStmt:
			_ = v
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "CREATE FUNCTION/PROCEDURE is a DDL operation with procedural logic",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		case *pg_query.Node_CreateTrigStmt:
			_ = v
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "CREATE TRIGGER adds implicit execution on DML — hidden complexity",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		case *pg_query.Node_DropStmt:
			checkCascadeDrop(v.DropStmt, cog)
			cog.Findings = append(cog.Findings, Finding{
				Rule:        "ddl-statement",
				Description: "DROP is a destructive DDL operation",
				Penalty:     PenaltyDDLStatement(),
				Category:    "cognitive_complexity",
			})
			cog.Score += PenaltyDDLStatement()
		}
		return true
	})
}

// checkCascadeDrop flags DROP ... CASCADE which recursively removes dependencies.
func checkCascadeDrop(stmt *pg_query.DropStmt, cog *DimensionScore) {
	if stmt == nil {
		return
	}
	if stmt.Behavior == pg_query.DropBehavior_DROP_CASCADE {
		cog.Findings = append(cog.Findings, Finding{
			Rule:        "cascade-drop",
			Description: "DROP CASCADE recursively removes all dependent objects — high risk",
			Penalty:     PenaltyCascadeDrop(),
			Category:    "cognitive_complexity",
		})
		cog.Score += PenaltyCascadeDrop()
	}
}
