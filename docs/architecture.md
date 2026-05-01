# Architecture

## Overview

sqlscore is structured as a three-layer pipeline:

```
SQL string → Parser → AST → Scorers → Report
```

### Package Layout

```
sqlscore/
├── cmd/sqlscore/     # CLI entry point
│   └── main.go       # Flag parsing, I/O, text/JSON output
├── parser/           # SQL parsing and AST traversal
│   └── parser.go     # Parse(), Walk(), Children()
├── scorer/           # Scoring engine
│   ├── scorer.go     # ScoreQuery(), Report, types
│   ├── efficiency.go # EfficiencyScorer
│   ├── memory_compute.go  # MemoryComputeScorer
│   └── cognitive.go  # CognitiveScorer
└── docs/             # Documentation
```

## Parser Layer

The parser wraps `pg_query_go` v5, which embeds PostgreSQL's actual parser via cgo. This means every SQL construct that PostgreSQL accepts is parseable — no grammar maintenance required.

The `Parse()` function returns a `*pg_query.ParseResult` containing a protobuf-based AST. The `Walk()` function provides depth-first traversal with a callback that receives each node and its depth. `Children()` maps each node type to its child nodes, enabling generic traversal without type-switching at every call site.

### Why a custom walker?

The pg_query protobuf AST uses a `Node` wrapper with a `oneof` field for each node type. There is no built-in traversal. Our `Children()` function exhaustively maps the node types we care about to their child references, making `Walk()` generic.

## Scorer Layer

Each scorer implements the `Scorer` interface:

```go
type Scorer interface {
    Score(tree *pg_query.ParseResult) DimensionScore
}
```

Scorers are independent — they walk the AST separately and produce findings with no shared state. `ScoreQuery()` runs all three and combines the results into a `Report`.

### EfficiencyScorer

Walks the AST looking for anti-patterns that prevent the query optimizer from using indexes or cause full table scans:

- **SELECT \***: Detected by checking `ResTarget.Val` for `ColumnRef` containing `A_Star`.
- **Missing predicates**: Counts `RangeVar` nodes in `FromClause`; flags if ≥2 without a `WhereClause`.
- **Correlated subqueries**: Checks `SubLink` nodes — `EXISTS` is always flagged, `IN/ANY/ALL` flagged when `Testexpr` is present.
- **Non-sargable predicates**: Walks `WhereClause` for `FuncCall` nodes wrapping `ColumnRef`. Handles PostgreSQL's schema-qualified names (e.g., `pg_catalog.btrim` for `TRIM()`).
- **DISTINCT as dedup**: Flags `DISTINCT` when the query also contains `JoinExpr`.

### MemoryComputeScorer

Targets operations that require materializing intermediate result sets:

- **Unbounded sort**: `SortClause` present without `LimitCount` or `LimitOffset`.
- **GROUP BY fan-out**: `GroupClause` present with aggregate functions in the target list.
- **Window functions**: `FuncCall` with an `Over` clause. Extra penalty when `PARTITION BY` is absent.
- **Cartesian products**: `JoinExpr` with no `Quals` and no `UsingClause`, or multiple `RangeVar` in FROM without WHERE.

### CognitiveScorer

Adapts cyclomatic complexity to SQL readability:

- **Subquery nesting**: `SubLink` and `RangeSubselect` nodes, with penalty multiplied by nesting depth.
- **Joins**: Each `JoinExpr` adds a flat penalty.
- **Boolean nesting**: `BoolExpr` nodes at depth > 0 within the WHERE/HAVING clause tree.
- **CTEs**: Each `CommonTableExpr` adds a flat penalty.
- **CASE expressions**: `CaseExpr` nodes in the target list.
- **Set operations**: `UNION/INTERSECT/EXCEPT` detected via `SelectStmt.Op`.

## CLI Layer

The CLI handles three input modes (flag, file, stdin), two output formats (text, JSON), and a verbose mode that expands individual findings. It's a thin wrapper around `scorer.ScoreQuery()`.

## Design Decisions

**Independent scorers**: Each dimension walks the AST independently. This is slightly less efficient than a single-pass walk but makes each scorer testable in isolation and trivial to add or remove.

**Conservative weights**: Penalty values are starting points. The intended calibration path is to run against a corpus of queries with known `EXPLAIN ANALYZE` output and adjust weights to correlate with actual performance impact.

**PostgreSQL-only**: We chose depth over breadth. pg_query_go gives us complete PostgreSQL grammar support. MySQL support via vitess/sqlparser is achievable with a dialect abstraction layer but was deferred to avoid premature generalization.
