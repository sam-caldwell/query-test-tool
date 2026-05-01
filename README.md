# sqlscore

Static analysis tool that parses arbitrary SQL queries and scores them across three dimensions: **efficiency**, **memory/compute cost**, and **cognitive complexity**.

Built on [pg_query_go](https://github.com/pganalyze/pg_query_go), which wraps PostgreSQL's own parser — so any SQL that Postgres accepts can be scored.

## Installation

```bash
go install github.com/sqlscore/cmd/sqlscore@latest
```

Or build from source:

```bash
git clone <repo>
cd sqlscore
go build -o sqlscore ./cmd/sqlscore
```

## Usage

```bash
# From a string
sqlscore -q "SELECT * FROM users ORDER BY name"

# From stdin
echo "SELECT * FROM users" | sqlscore

# From a file
sqlscore -f query.sql

# Positional argument
sqlscore "SELECT id FROM users WHERE id = 1"

# JSON output
sqlscore -q "SELECT * FROM users" -format json

# Verbose (show all findings)
sqlscore -q "SELECT * FROM users ORDER BY name" -v
```

## Example Output

```
SQL Query Score Report
======================

Total Score: 25 (fair)

  efficiency:             15  (2 finding(s))
    [+5] select-star               SELECT * prevents index-only scans and fetches unnecessary columns
    [+10] non-sargable              Function LOWER() on column prevents index usage
  memory_compute:          8  (1 finding(s))
    [+8] unbounded-sort            ORDER BY without LIMIT requires materializing and sorting the entire result set
  cognitive_complexity:    2  (1 finding(s))
    [+2] join                      Each JOIN adds a relationship to reason about
```

## Scoring Dimensions

### Efficiency (anti-patterns that prevent optimal execution)

| Rule | Penalty | Description |
|------|---------|-------------|
| `select-star` | 5 | `SELECT *` prevents index-only scans |
| `missing-predicate` | 10 | Multiple tables in FROM without WHERE |
| `correlated-subquery` | 15 | Subquery that executes per outer row |
| `non-sargable` | 10 | Function on column in WHERE prevents index usage |
| `distinct-dedup` | 8 | DISTINCT with JOIN suggests join duplication |

### Memory/Compute (operations requiring materialization)

| Rule | Penalty | Description |
|------|---------|-------------|
| `unbounded-sort` | 8 | ORDER BY without LIMIT |
| `group-by-fanout` | 5 | GROUP BY with aggregation |
| `window-function` | 6 | Window function (+ 4 without PARTITION BY) |
| `cartesian-product` | 15 | CROSS JOIN or implicit cross join |

### Cognitive Complexity (readability and reasoning cost)

| Rule | Penalty | Description |
|------|---------|-------------|
| `subquery-nesting` | 3 × depth | Each nesting level multiplies penalty |
| `join` | 2 | Per join in the query |
| `boolean-nesting` | 2 × depth | Nested AND/OR expressions |
| `cte` | 2 | Per Common Table Expression |
| `case-expression` | 2 | Per CASE expression |
| `set-operation` | 3 | UNION/INTERSECT/EXCEPT |

## Grades

| Score | Grade |
|-------|-------|
| 0 | Excellent |
| 1–10 | Good |
| 11–25 | Fair |
| 26–50 | Poor |
| 51+ | Critical |

## Architecture

See [docs/architecture.md](docs/architecture.md) for the system design and [docs/scoring.md](docs/scoring.md) for the scoring methodology.

## Library Usage

```go
import "github.com/sqlscore/scorer"

report, err := scorer.ScoreQuery("SELECT * FROM users ORDER BY name")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Total: %d\n", report.TotalScore)
for _, f := range report.Efficiency.Findings {
    fmt.Printf("  [%s] %s\n", f.Rule, f.Description)
}
```

## Testing

```bash
go test ./... -v
go test ./parser/... ./scorer/... -cover  # parser: 100%, scorer: 98.6%
```

## References

- Halstead, M. H. (1977). *Elements of Software Science*. Elsevier.
- McCabe, T. J. (1976). A complexity measure. *IEEE Transactions on Software Engineering*, SE-2(4), 308–320.
- Trzciński, K. (2021). pg_query_go [Software]. pganalyze. https://github.com/pganalyze/pg_query_go
