# Scoring Methodology

## Theoretical Foundation

sqlscore draws from two established complexity models, adapted to SQL:

### McCabe's Cyclomatic Complexity (1976)

McCabe measured software complexity by counting independent paths through a program's control flow graph. For SQL, we adapt this to count structural decision points: each JOIN adds a relationship, each subquery adds a scope, each boolean nesting adds a conditional branch. The cognitive complexity dimension directly implements this model.

### Halstead's Software Science (1977)

Halstead measured complexity by counting operators and operands. Our efficiency and memory/compute dimensions adapt this by counting AST node types that correspond to expensive operations — function calls on indexed columns (operators that defeat indexing), GROUP BY with aggregation (operators that require materialization), window functions (operators that maintain state across rows).

## Scoring Model

### Additive Penalties

Each finding adds a penalty to its dimension's score. The total score is the sum of all three dimensions. A score of 0 means no issues detected.

### Depth-Multiplied Penalties

Subquery nesting and boolean nesting use depth-multiplied penalties: a subquery at depth 1 costs 3, at depth 2 costs 6, at depth 3 costs 9. This reflects the cognitive reality that each level of nesting compounds the difficulty of reasoning about the query.

### Grades

| Score Range | Grade | Interpretation |
|-------------|-------|----------------|
| 0 | Excellent | No anti-patterns detected |
| 1–10 | Good | Minor issues, generally acceptable |
| 11–25 | Fair | Multiple issues worth addressing |
| 26–50 | Poor | Significant problems, review recommended |
| 51+ | Critical | Likely performance problems, refactor |

## Efficiency Rules

### select-star (penalty: 5)

`SELECT *` fetches all columns, defeating index-only scans and increasing I/O. The penalty is moderate because it's sometimes intentional (e.g., in ad-hoc queries or when all columns are needed).

**Detection**: `ResTarget.Val` is a `ColumnRef` containing an `A_Star` node.

### missing-predicate (penalty: 10)

Multiple tables in FROM without a WHERE clause strongly suggests a missing join predicate, producing an unintentional Cartesian product.

**Detection**: Count `RangeVar` nodes in `FromClause` ≥ 2 with nil `WhereClause`.

### correlated-subquery (penalty: 15)

Correlated subqueries execute once per outer row in the worst case, turning an O(n) scan into O(n²). The high penalty reflects this multiplicative cost.

**Detection**: `SubLink` nodes with type `EXISTS`, or `ANY/ALL/EXPR` with a non-nil `Testexpr`.

### non-sargable (penalty: 10)

Applying a function to a column in a WHERE predicate (e.g., `WHERE LOWER(email) = 'test'`) prevents the optimizer from using an index on that column. The query must scan every row and apply the function.

**Detection**: `FuncCall` wrapping a `ColumnRef` inside `AExpr` nodes within the `WhereClause`. Handles PostgreSQL's schema-qualified function names (e.g., `pg_catalog.btrim` for `TRIM()`).

### distinct-dedup (penalty: 8)

`DISTINCT` combined with `JOIN` often indicates the join is producing duplicate rows that should be eliminated by fixing the join condition, not by adding DISTINCT as a band-aid.

**Detection**: Non-empty `DistinctClause` with `JoinExpr` in `FromClause`.

## Memory/Compute Rules

### unbounded-sort (penalty: 8)

`ORDER BY` without `LIMIT` requires materializing and sorting the entire result set in memory. For large tables, this can consume significant memory and time.

**Detection**: Non-empty `SortClause` with nil `LimitCount` and nil `LimitOffset`.

### group-by-fanout (penalty: 5)

`GROUP BY` with aggregation requires the database to build hash tables or sort the data by grouping keys. The penalty is moderate because GROUP BY is often necessary and well-optimized.

**Detection**: Non-empty `GroupClause` with aggregate function calls (`COUNT`, `SUM`, `AVG`, `MIN`, `MAX`, etc.) in the target list.

### window-function (penalty: 6 or 10)

Window functions maintain state across the partition. Without `PARTITION BY`, the window operates over the entire result set (penalty: 10). With `PARTITION BY`, the scope is bounded (penalty: 6).

**Detection**: `FuncCall` with non-nil `Over`. Extra penalty when `Over.PartitionClause` is empty.

### cartesian-product (penalty: 15)

Cartesian products produce O(n × m) rows. This is the highest memory/compute penalty because the cost is multiplicative.

**Detection**: `JoinExpr` with no `Quals` and no `UsingClause` (explicit CROSS JOIN), or multiple `RangeVar` in FROM without WHERE (implicit cross join).

## Cognitive Complexity Rules

### subquery-nesting (penalty: 3 × depth)

Each level of subquery nesting adds a scope that the reader must hold in working memory. The depth multiplier reflects compounding difficulty.

**Detection**: `SubLink` and `RangeSubselect` nodes. The scorer tracks nesting depth and multiplies the base penalty.

### join (penalty: 2)

Each JOIN adds a table relationship to reason about. The penalty is flat because JOINs don't compound — they're additive relationships.

**Detection**: `JoinExpr` nodes in the FROM clause, counted recursively for nested joins.

### boolean-nesting (penalty: 2 × depth)

Nested boolean expressions (e.g., `(a AND b) OR (c AND d)`) require the reader to track operator precedence and grouping. Only nesting beyond depth 0 is penalized — flat `AND`/`OR` is readable.

**Detection**: `BoolExpr` nodes within `WhereClause` and `HavingClause`, with depth tracking.

### cte (penalty: 2)

Each CTE adds a named scope. While CTEs improve readability by decomposing queries, each one is an additional "definition" the reader must understand.

**Detection**: `CommonTableExpr` nodes in `WithClause`.

### case-expression (penalty: 2)

CASE adds conditional branching logic to the result set.

**Detection**: `CaseExpr` nodes in the target list.

### set-operation (penalty: 3)

UNION, INTERSECT, and EXCEPT combine result sets from multiple queries, adding structural complexity.

**Detection**: `SelectStmt.Op` != `SETOP_NONE`.

## Calibration

The scoring weights are deliberately conservative starting points. To calibrate for your workload:

1. Collect a corpus of production queries with `EXPLAIN ANALYZE` output
2. Score each query with sqlscore
3. Correlate scores with actual execution time, rows examined, and memory usage
4. Adjust weights to maximize correlation

The cognitive complexity dimension should be calibrated against human readability assessments rather than runtime metrics.

## Limitations

- **Static analysis only**: Scores reflect query structure, not table sizes, data distribution, or available indexes. A `SELECT *` on a 10-row lookup table is not the same as on a billion-row fact table.
- **No schema awareness**: Without knowing which columns are indexed, non-sargable detection can only flag the structural pattern, not confirm index defeat.
- **PostgreSQL grammar only**: The parser accepts PostgreSQL syntax. MySQL, SQL Server, and other dialects require separate parser integration.
