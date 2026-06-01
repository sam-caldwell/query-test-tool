package parser

import (
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Targeted coverage tests for Children branches.

func TestChildren_WindowDefDirect(t *testing.T) {
	node := &pg_query.Node{Node: &pg_query.Node_WindowDef{WindowDef: &pg_query.WindowDef{
		PartitionClause: []*pg_query.Node{
			{Node: &pg_query.Node_ColumnRef{ColumnRef: &pg_query.ColumnRef{}}},
		},
		OrderClause: []*pg_query.Node{
			{Node: &pg_query.Node_SortBy{SortBy: &pg_query.SortBy{}}},
		},
	}}}
	children := Children(node)
	if len(children) != 2 {
		t.Errorf("WindowDef should have 2 children, got %d", len(children))
	}
}

func TestChildren_WithClauseDirect(t *testing.T) {
	node := &pg_query.Node{Node: &pg_query.Node_WithClause{WithClause: &pg_query.WithClause{
		Ctes: []*pg_query.Node{
			{Node: &pg_query.Node_CommonTableExpr{CommonTableExpr: &pg_query.CommonTableExpr{}}},
		},
	}}}
	children := Children(node)
	if len(children) != 1 {
		t.Errorf("WithClause should have 1 child, got %d", len(children))
	}
}

func TestChildren_FloatDirect(t *testing.T) {
	node := &pg_query.Node{Node: &pg_query.Node_Float{Float: &pg_query.Float{Fval: "1.5"}}}
	children := Children(node)
	if len(children) != 0 {
		t.Error("Float should have no children")
	}
}

func TestChildren_BooleanDirect(t *testing.T) {
	node := &pg_query.Node{Node: &pg_query.Node_Boolean{Boolean: &pg_query.Boolean{Boolval: true}}}
	children := Children(node)
	if len(children) != 0 {
		t.Error("Boolean should have no children")
	}
}

func TestChildren_IntegerDirect(t *testing.T) {
	node := &pg_query.Node{Node: &pg_query.Node_Integer{Integer: &pg_query.Integer{Ival: 1}}}
	children := Children(node)
	if len(children) != 0 {
		t.Error("Integer should have no children")
	}
}

func TestChildren_ParamRefDirect(t *testing.T) {
	node := &pg_query.Node{Node: &pg_query.Node_ParamRef{ParamRef: &pg_query.ParamRef{Number: 1}}}
	children := Children(node)
	if len(children) != 0 {
		t.Error("ParamRef should have no children")
	}
}
