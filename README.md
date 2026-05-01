# sqlscore

Static analysis tool that parses arbitrary SQL queries and scores them across three dimensions: **efficiency**, **memory/compute cost**, and **cognitive complexity**. Includes a calibration system that derives optimal scoring weights empirically by running queries against PostgreSQL with EXPLAIN ANALYZE.

Built on [pg_query_go](https://github.com/pganalyze/pg_query_go), which wraps PostgreSQL's own parser — so any SQL that Postgres accepts can be scored.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Usage](#usage)
- [Scoring Dimensions](#scoring-dimensions)
- [Weight Calibration](#weight-calibration)
- [Build System](#build-system)
- [Project Structure](#project-structure)
- [Testing](#testing)
- [Architecture](#architecture)
- [References](#references)

## Installation

### From Source

```bash
git clone <repo> ~/git/query-test-tool
cd ~/git/query-test-tool
make build      # builds to bin/
make install    # copies to ~/.bin/
```

### Go Install

```bash
go install github.com/sqlscore/cmd/sqlscore@latest
go install github.com/sqlscore/cmd/calibrate@latest
```

## Quick Start

```bash
# Score a query
sqlscore -q "SELECT * FROM users ORDER BY name"

# Verbose output with individual findings
sqlscore -q "SELECT * FROM users WHERE LOWER(email) = 'test'" -v

# JSON output for programmatic use
sqlscore -q "SELECT * FROM users" -format json

# Pipe from file or stdin
cat query.sql | sqlscore
sqlscore -f query.sql
```

## Usage

### sqlscore

```
Usage: sqlscore [options] [SQL]

Score SQL queries for efficiency, memory/compute cost, and cognitive complexity.

Options:
  -q, -query     SQL query to score
  -f, -file      File containing SQL query
  -format        Output format: text or json (default: text)
  -v, -verbose   Show detailed findings
  -version       Show version and weights info

Input sources (in priority order):
  1. -q / -query flag
  2. -f / -file flag
  3. Positional arguments
  4. stdin (if piped)
```

### Output Example

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

### JSON Output

```json
{
  "sql": "SELECT * FROM users",
  "total_score": 5,
  "efficiency": {
    "name": "efficiency",
    "score": 5,
    "findings": [
      {
        "rule": "select-star",
        "description": "SELECT * prevents index-only scans and fetches unnecessary columns",
        "penalty": 5,
        "category": "efficiency"
      }
    ]
  },
  "memory_compute": { "name": "memory_compute", "score": 0, "findings": null },
  "cognitive_complexity": { "name": "cognitive_complexity", "score": 0, "findings": null }
}
```

## Scoring Dimensions

### Efficiency (anti-patterns that prevent optimal execution)

| Rule | Default Weight | Description |
|------|---------------|-------------|
| `select-star` | 5 | `SELECT *` prevents index-only scans |
| `missing-predicate` | 10 | Multiple tables in FROM without WHERE |
| `correlated-subquery` | 15 | Subquery that executes per outer row |
| `non-sargable` | 10 | Function on column in WHERE prevents index usage |
| `distinct-dedup` | 8 | DISTINCT with JOIN suggests join duplication |

### Memory/Compute (operations requiring materialization)

| Rule | Default Weight | Description |
|------|---------------|-------------|
| `unbounded-sort` | 8 | ORDER BY without LIMIT |
| `group-by-fanout` | 5 | GROUP BY with aggregation |
| `window-function` | 6 | Window function (+4 without PARTITION BY) |
| `cartesian-product` | 15 | CROSS JOIN or implicit cross join |

### Cognitive Complexity (readability and reasoning cost)

| Rule | Default Weight | Description |
|------|---------------|-------------|
| `subquery-nesting` | 3 × depth | Each nesting level multiplies penalty |
| `join` | 2 | Per join in the query |
| `boolean-nesting` | 2 × depth | Nested AND/OR expressions |
| `cte` | 2 | Per Common Table Expression |
| `case-expression` | 2 | Per CASE expression |
| `set-operation` | 3 | UNION/INTERSECT/EXCEPT |

### Grades

| Score | Grade |
|-------|-------|
| 0 | Excellent |
| 1–10 | Good |
| 11–25 | Fair |
| 26–50 | Poor |
| 51+ | Critical |

## Weight Calibration

Scoring weights are stored in `scorer/weights.json` and embedded at build time. The `calibrate` tool derives optimal weights empirically.

### How It Works

1. **Generate 10,000 schemas** — 5 domain archetypes × systematically applied mutations (dropped indexes, widened tables, removed FKs, textified columns)
2. **Populate with data** — bulk `generate_series`-based insertion with realistic patterns
3. **Generate 1,000,000 queries** — 18 templates per antipattern, parameterized per schema
4. **Run EXPLAIN ANALYZE** — concurrent execution against optimal and degraded schemas
5. **OLS regression** — fits `log(cost_ratio) = Σ βᵢ × finding_count_i` to derive weights
6. **Write weights** — outputs `scorer/weights.json`; rebuild `cmd/sqlscore` to embed

### Running Calibration

```bash
# Prerequisites
createdb sqlscore_calibrate

# Full pipeline (generates schemas, queries, runs EXPLAIN, computes weights)
./bin/calibrate -dsn "postgres://localhost:5432/sqlscore_calibrate?sslmode=disable"

# Or run phases independently
./bin/calibrate -phase init
./bin/calibrate -phase generate -schemas 1000 -queries 100000 -rows 500
./bin/calibrate -phase run -workers 16
./bin/calibrate -phase calculate -output scorer/weights.json

# Rebuild sqlscore to embed new weights
make build
```

### Calibration Options

```
  -dsn          PostgreSQL connection string
  -phase        Pipeline phase: init, generate, run, calculate, all (default: all)
  -schemas      Target schema count (default: 10000)
  -queries      Target query count (default: 1000000)
  -rows         Rows per table (default: 1000)
  -workers      Concurrent EXPLAIN workers (default: 8)
  -timeout      Per-query timeout in ms (default: 5000)
  -output       Output weights file (default: scorer/weights.json)
```

### Calibration Output

```json
{
  "version": 1,
  "description": "Calibrated weights from 1847293 samples (R²=0.7341)",
  "r_squared": 0.7341,
  "sample_size": 1847293,
  "generated_at": "2026-05-01T15:30:00Z",
  "weights": {
    "select-star": 5,
    "missing-predicate": 12,
    "correlated-subquery": 18,
    "non-sargable": 11,
    "distinct-dedup": 7,
    "unbounded-sort": 9,
    "group-by-fanout": 4,
    "window-function": 6,
    "window-no-partition-extra": 5,
    "cartesian-product": 19,
    "subquery-nesting": 3,
    "join": 2,
    "boolean-nesting": 2,
    "cte": 1,
    "case-expression": 1,
    "set-operation": 3
  }
}
```

## Build System

### Makefile Targets

| Target | Description |
|--------|-------------|
| `make clean` | Remove and recreate `bin/` |
| `make lint` | Run `go vet -v ./...` and `govulncheck` |
| `make build` | Build binaries to `bin/` with version/commit embedded via ldflags |
| `make install` | Copy binaries from `bin/` to `~/.bin/` |
| `make test` | Run unit tests → integration tests → e2e tests (in order) |
| `make release` | Bump patch version (alias for `make release/patch`) |
| `make release/patch` | Bump patch version (0.1.0 → 0.1.1), commit, and tag |
| `make release/minor` | Bump minor version (0.1.0 → 0.2.0), commit, and tag |
| `make release/major` | Bump major version (0.1.0 → 1.0.0), commit, and tag |
| `make all` | Run clean → lint → build → test |
| `make help` | Show available targets |

### Build Workflow

```bash
# Development cycle
make lint           # check for issues
make build          # compile
make test           # verify

# Release
make release/minor  # bumps VERSION, commits, tags
git push && git push --tags
```

### Embedded Weights

The `scorer/weights.json` file is embedded into the `sqlscore` binary at compile time using Go's `//go:embed` directive. This means:

1. Default weights ship with the binary (no external files needed)
2. Running `calibrate` updates `scorer/weights.json`
3. Rebuilding with `make build` picks up the new weights
4. The binary is fully self-contained

## Project Structure

```
query-test-tool/
├── Makefile                  # Build, test, release targets
├── VERSION                   # Semantic version (read by Makefile)
├── README.md                 # This file
├── go.mod / go.sum           # Go module dependencies
├── scorer/
│   ├── weights.json          # Embedded scoring weights (updated by calibrate)
│   ├── weights.go            # go:embed loader
│   ├── scorer.go             # ScoreQuery(), Report, types
│   ├── efficiency.go         # EfficiencyScorer
│   ├── memory_compute.go     # MemoryComputeScorer
│   ├── cognitive.go          # CognitiveScorer
│   └── *_test.go             # Unit tests (98.6% coverage)
├── parser/
│   ├── parser.go             # Parse(), Walk(), Children()
│   └── *_test.go             # Unit tests (100% coverage)
├── calibrate/
│   ├── types.go              # Shared types
│   ├── archetype.go          # 5 domain archetypes
│   ├── mutation.go           # Schema mutation generators
│   ├── schemagen.go          # Schema family generation
│   ├── datagen.go            # Data population
│   ├── querygen.go           # Query generation (18 templates)
│   ├── runner.go             # EXPLAIN execution
│   ├── regression.go         # OLS ridge regression
│   ├── pipeline.go           # Pipeline orchestration
│   ├── db.go                 # Database operations
│   └── *_test.go             # Unit tests
├── cmd/
│   ├── sqlscore/main.go      # CLI for scoring queries
│   └── calibrate/main.go     # CLI for weight calibration
└── docs/
    ├── architecture.md       # System design
    ├── scoring.md            # Scoring methodology
    └── calibration.md        # Calibration methodology
```

## Testing

```bash
# All tests
make test

# Unit tests only (no DB required)
make test/unit

# With coverage
go test ./parser/... ./scorer/... -cover
# parser: 100%, scorer: 98.6%

# Race detection
go test ./... -race
```

### Test Organization

- **Unit tests** (`test/unit`): Parser, scorer, and calibrate logic (schema gen, query gen, regression). No external dependencies.
- **Integration tests** (`test/integration`): Full package tests including CLI subprocess tests. Tagged with `integration` for DB-dependent tests.
- **E2E tests** (`test/e2e`): Built binary execution against the CLI interface.

## Architecture

See [docs/architecture.md](docs/architecture.md) for detailed system design.

### Key Design Decisions

- **Embedded weights**: Weights are compiled into the binary via `//go:embed`. No config files at runtime.
- **Independent scorers**: Each dimension walks the AST independently for testability.
- **Ridge regression**: Handles sparse features (most queries trigger 1-3 rules out of 15).
- **PostgreSQL parser**: Uses libpg_query via cgo for complete grammar support.
- **Schema families**: Optimal/degraded pairs enable cost ratio comparison for calibration.

## Library Usage

```go
import "github.com/sqlscore/scorer"

report, err := scorer.ScoreQuery("SELECT * FROM users ORDER BY name")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Total: %d (%s)\n", report.TotalScore, grade(report.TotalScore))
for _, f := range report.Efficiency.Findings {
    fmt.Printf("  [%s] %s\n", f.Rule, f.Description)
}

// Access current weights
w := scorer.Weights()
fmt.Printf("select-star penalty: %d\n", w.Weights["select-star"])
```

## References

- Halstead, M. H. (1977). *Elements of Software Science*. Elsevier.
- McCabe, T. J. (1976). A complexity measure. *IEEE Transactions on Software Engineering*, SE-2(4), 308–320.
- Trzciński, K. (2021). pg_query_go [Software]. pganalyze. https://github.com/pganalyze/pg_query_go
