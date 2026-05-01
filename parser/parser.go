// Package parser wraps pg_query_go to parse SQL into a walkable AST.
package parser

import (
	"fmt"

	pg_query "github.com/pganalyze/pg_query_go/v5"
)

// Parse parses a SQL string and returns the pg_query ParseResult.
func Parse(sql string) (*pg_query.ParseResult, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return tree, nil
}

// WalkFunc is called for each node during AST traversal.
// depth indicates the nesting level (0 = top-level statement).
// Return false to stop walking children of this node.
type WalkFunc func(node *pg_query.Node, depth int) bool

// Walk traverses the AST rooted at node, calling fn for each node.
func Walk(node *pg_query.Node, depth int, fn WalkFunc) {
	if node == nil {
		return
	}
	if !fn(node, depth) {
		return
	}
	for _, child := range Children(node) {
		Walk(child, depth+1, fn)
	}
}

// WalkNodes traverses a slice of nodes.
func WalkNodes(nodes []*pg_query.Node, depth int, fn WalkFunc) {
	for _, n := range nodes {
		Walk(n, depth, fn)
	}
}

// Children returns the direct child nodes of a given node.
func Children(node *pg_query.Node) []*pg_query.Node {
	if node == nil {
		return nil
	}
	var children []*pg_query.Node

	switch n := node.Node.(type) {
	case *pg_query.Node_SelectStmt:
		s := n.SelectStmt
		children = appendNodes(children, s.TargetList...)
		children = appendNodes(children, s.FromClause...)
		children = appendNode(children, s.WhereClause)
		children = appendNodes(children, s.GroupClause...)
		children = appendNode(children, s.HavingClause)
		children = appendNodes(children, s.WindowClause...)
		children = appendNodes(children, s.SortClause...)
		children = appendNode(children, s.LimitCount)
		children = appendNode(children, s.LimitOffset)
		children = appendNodes(children, s.DistinctClause...)
		children = appendNodes(children, s.ValuesLists...)
		if s.WithClause != nil {
			for _, cte := range s.WithClause.Ctes {
				children = appendNode(children, cte)
			}
		}
		if s.Larg != nil {
			children = append(children, nodeFromSelectStmt(s.Larg))
		}
		if s.Rarg != nil {
			children = append(children, nodeFromSelectStmt(s.Rarg))
		}

	case *pg_query.Node_JoinExpr:
		j := n.JoinExpr
		children = appendNode(children, j.Larg)
		children = appendNode(children, j.Rarg)
		children = appendNode(children, j.Quals)
		children = appendNodes(children, j.UsingClause...)

	case *pg_query.Node_BoolExpr:
		b := n.BoolExpr
		children = appendNodes(children, b.Args...)

	case *pg_query.Node_SubLink:
		sl := n.SubLink
		children = appendNode(children, sl.Testexpr)
		children = appendNode(children, sl.Subselect)
		children = appendNodes(children, sl.OperName...)

	case *pg_query.Node_FuncCall:
		fc := n.FuncCall
		children = appendNodes(children, fc.Funcname...)
		children = appendNodes(children, fc.Args...)
		children = appendNode(children, fc.AggFilter)
		children = appendNodes(children, fc.AggOrder...)
		if fc.Over != nil {
			children = appendNodes(children, fc.Over.PartitionClause...)
			children = appendNodes(children, fc.Over.OrderClause...)
		}

	case *pg_query.Node_ResTarget:
		rt := n.ResTarget
		children = appendNode(children, rt.Val)
		children = appendNodes(children, rt.Indirection...)

	case *pg_query.Node_ColumnRef:
		cr := n.ColumnRef
		children = appendNodes(children, cr.Fields...)

	case *pg_query.Node_AExpr:
		ae := n.AExpr
		children = appendNode(children, ae.Lexpr)
		children = appendNode(children, ae.Rexpr)
		children = appendNodes(children, ae.Name...)

	case *pg_query.Node_TypeCast:
		tc := n.TypeCast
		children = appendNode(children, tc.Arg)

	case *pg_query.Node_CaseExpr:
		ce := n.CaseExpr
		children = appendNode(children, ce.Arg)
		children = appendNodes(children, ce.Args...)
		children = appendNode(children, ce.Defresult)

	case *pg_query.Node_CaseWhen:
		cw := n.CaseWhen
		children = appendNode(children, cw.Expr)
		children = appendNode(children, cw.Result)

	case *pg_query.Node_CoalesceExpr:
		co := n.CoalesceExpr
		children = appendNodes(children, co.Args...)

	case *pg_query.Node_NullTest:
		nt := n.NullTest
		children = appendNode(children, nt.Arg)

	case *pg_query.Node_SortBy:
		sb := n.SortBy
		children = appendNode(children, sb.Node)
		children = appendNodes(children, sb.UseOp...)

	case *pg_query.Node_WindowDef:
		wd := n.WindowDef
		children = appendNodes(children, wd.PartitionClause...)
		children = appendNodes(children, wd.OrderClause...)

	case *pg_query.Node_RangeSubselect:
		rs := n.RangeSubselect
		children = appendNode(children, rs.Subquery)

	case *pg_query.Node_CommonTableExpr:
		cte := n.CommonTableExpr
		children = appendNode(children, cte.Ctequery)

	case *pg_query.Node_WithClause:
		wc := n.WithClause
		children = appendNodes(children, wc.Ctes...)

	case *pg_query.Node_InsertStmt:
		is := n.InsertStmt
		children = appendNode(children, is.SelectStmt)
		children = appendNodes(children, is.ReturningList...)

	case *pg_query.Node_UpdateStmt:
		us := n.UpdateStmt
		children = appendNodes(children, us.TargetList...)
		children = appendNode(children, us.WhereClause)
		children = appendNodes(children, us.FromClause...)
		children = appendNodes(children, us.ReturningList...)

	case *pg_query.Node_DeleteStmt:
		ds := n.DeleteStmt
		children = appendNode(children, ds.WhereClause)
		children = appendNodes(children, ds.UsingClause...)
		children = appendNodes(children, ds.ReturningList...)

	case *pg_query.Node_RangeVar:
		// leaf node - no children

	case *pg_query.Node_String_:
		// leaf node

	case *pg_query.Node_Integer:
		// leaf node

	case *pg_query.Node_Float:
		// leaf node

	case *pg_query.Node_Boolean:
		// leaf node

	case *pg_query.Node_AStar:
		// leaf node

	case *pg_query.Node_AConst:
		// leaf node

	case *pg_query.Node_ParamRef:
		// leaf node

	case *pg_query.Node_List:
		l := n.List
		children = appendNodes(children, l.Items...)
	}

	return children
}

func appendNode(children []*pg_query.Node, node *pg_query.Node) []*pg_query.Node {
	if node != nil {
		return append(children, node)
	}
	return children
}

func appendNodes(children []*pg_query.Node, nodes ...*pg_query.Node) []*pg_query.Node {
	for _, n := range nodes {
		if n != nil {
			children = append(children, n)
		}
	}
	return children
}

func nodeFromSelectStmt(s *pg_query.SelectStmt) *pg_query.Node {
	return &pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: s}}
}
