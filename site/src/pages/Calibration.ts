import {createElement} from '@asymmetric-effort/specifyjs';

export function Calibration() {

    return createElement('div', null,
        createElement('h1', null, 'Weight Calibration'),
        createElement('p', null,
            'Scoring weights are not guessed — they are derived from real PostgreSQL query execution costs. ',
            'The calibration pipeline measures how much each antipattern actually costs by comparing queries with and without the pattern.',
        ),

        createElement('h2', null, 'Pipeline'),
        createElement('pre', {},
            '┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐\n' +
            '│  Schema Gen │ →  │  Data Gen   │ →  │ Query Gen   │ →  │  EXPLAIN    │\n' +
            '│  5K schemas │    │ 70K rows/tbl│    │  1M queries  │    │  Runner     │\n' +
            '└─────────────┘    └─────────────┘    └─────────────┘    └──────┬──────┘\n' +
            '                                                                │\n' +
            '                                                                ▼\n' +
            '                                                        ┌─────────────┐\n' +
            '                                                        │   Paired    │\n' +
            '                                                        │ Comparison  │\n' +
            '                                                        └─────────────┘\n' +
            '                                                                │\n' +
            '                                                                ▼\n' +
            '                                                       src/scorer/weights.json\n',
        ),

        createElement('h2', null, 'Domain Archetypes'),
        createElement('p', null,
            '4 heavyweight merged domains provide realistic multi-layer schemas — 84 tables, 912 columns, 316 indexes, 169 foreign keys total:',
        ),
        table([
            ['Domain', 'Tables', 'Columns', 'Indexes', 'FKs', 'Description'],
            ['cash_accounting', '15', '186', '54', '29', 'Cash-basis CPA system with soft-delete, fiscal periods, payroll'],
            ['accrual_accounting', '29', '318', '129', '75', 'Full double-entry GL with GAAP/IFRS, amortization schedules'],
            ['healthcare', '25', '268', '86', '47', 'Clinical system with encounters, vitals, prescriptions, labs'],
            ['supply_chain', '15', '140', '47', '18', 'Warehouse, procurement, shipments, quality control'],
        ]),

        createElement('h2', null, 'Schema Mutations'),
        createElement('p', null, 'Each domain is mutated to produce schema variants that exercise specific antipatterns:'),
        createElement('ul', null,
            createElement('li', null, 'Drop individual indexes (tests non-sargable, unbounded-sort)'),
            createElement('li', null, 'Drop all indexes (full scan behavior)'),
            createElement('li', null, 'Drop foreign keys (missing-predicate)'),
            createElement('li', null, 'Widen tables with extra columns (SELECT * cost)'),
            createElement('li', null, 'Textify numeric/date columns (type coercion)'),
            createElement('li', null, 'Remove NOT NULL constraints'),
            createElement('li', null, 'Denormalize tables (merge child into parent)'),
            createElement('li', null, 'Add redundant cached columns'),
        ),

        createElement('h2', null, 'Data Generation'),
        createElement('p', null,
            '70,000 base rows per table with tiered multipliers that push high-volume tables past memory thresholds where the query planner makes meaningfully different decisions:',
        ),
        createElement('ul', null,
            createElement('li', null, '1x — root/parent tables (70K rows)'),
            createElement('li', null, '3x — child tables with outbound FKs (210K rows)'),
            createElement('li', null, '5x — hub tables with 4+ inbound FKs (350K rows)'),
            createElement('li', null, '10x — BIGSERIAL high-volume tables like events, readings, audit logs (700K rows)'),
        ),
        createElement('p', null, 'Data characteristics:'),
        createElement('ul', null,
            createElement('li', null, 'High-cardinality values with skewed (Zipfian) distributions'),
            createElement('li', null, '10% sporadic NULLs on nullable columns'),
            createElement('li', null, 'Realistic text patterns (emails, statuses, timestamps)'),
            createElement('li', null, 'FK-consistent references using modular cycling'),
        ),

        createElement('h2', null, 'Query Templates'),
        createElement('p', null,
            '44 query templates generate paired queries (antipattern vs. control) covering all scoring rules. ',
            'Templates include JSONB operations (containment, extraction, path queries, aggregation), ',
            'window functions, CTEs, recursive CTEs, lateral joins, grouping sets, and more.',
        ),
        createElement('p', null, 'For each antipattern, the generator produces a "bad" query and a control:'),
        createElement('ul', null,
            createElement('li', null, 'select_star vs select_columns (same table, * vs named cols)'),
            createElement('li', null, 'non_sargable vs sargable (LOWER(col) = x vs col = x)'),
            createElement('li', null, 'unbounded_sort vs bounded_sort (ORDER BY vs ORDER BY LIMIT)'),
            createElement('li', null, 'exists_subquery vs proper_join'),
            createElement('li', null, 'distinct_join vs proper_join'),
            createElement('li', null, 'jsonb_containment, jsonb_extract, jsonb_path, jsonb_agg'),
        ),
        createElement('p', null,
            'Weight = median(cost_antipattern / cost_control), scaled to a 1-25 range. ',
            'This directly measures how much worse the antipattern is compared to the correct pattern.',
        ),

        createElement('h2', null, 'Batch-and-Drop Pipeline'),
        createElement('p', null,
            'The calibration pipeline processes schemas in configurable batches to limit peak disk usage. ',
            'Each batch creates schemas, runs EXPLAIN ANALYZE, stores results, then drops the data schemas before the next batch.',
        ),
        createElement('ul', null,
            createElement('li', null, 'UNLOGGED tables for fast bulk inserts (data is disposable)'),
            createElement('li', null, 'Deferred index creation — tables are populated first, indexes built after on existing data'),
            createElement('li', null, 'Native table partitioning on results (one partition per batch)'),
            createElement('li', null, 'Fully reentrant — the DB is the source of truth; interrupted runs resume from exact point of interruption'),
            createElement('li', null, 'Graceful stop on SIGINT/SIGTERM — finishes current batch, persists results, then exits cleanly'),
        ),

        createElement('h2', null, 'Running Calibration'),
        createElement('pre', {},
            '# Prerequisites\n' +
            'createdb sqlscore_calibrate\n\n' +
            '# Full pipeline (local PostgreSQL)\n' +
            './bin/calibrate -dsn "postgres:///sqlscore_calibrate?sslmode=disable"\n\n' +
            '# Remote PostgreSQL server\n' +
            './bin/calibrate -host db.example.com -port 5432 -user calibrate \\\n' +
            '  -password secret -dbname sqlscore_calibrate -sslmode require\n\n' +
            '# Custom batch size and timeout\n' +
            './bin/calibrate -batch-size 20 -timeout 10000\n\n' +
            '# Compressed log output\n' +
            './bin/calibrate -logfile calibration.log.gz\n\n' +
            '# Include your own business schema\n' +
            './bin/calibrate -schema-file ./my_app_schema.sql\n\n' +
            '# Rebuild with new weights\n' +
            'make build\n',
        ),

        createElement('h2', null, 'CLI Flags'),
        table([
            ['Flag', 'Default', 'Description'],
            ['-dsn', '', 'Full PostgreSQL connection string (overrides individual flags)'],
            ['-host', 'localhost', 'PostgreSQL host'],
            ['-port', '5432', 'PostgreSQL port'],
            ['-user', '', 'PostgreSQL user'],
            ['-password', '', 'PostgreSQL password'],
            ['-dbname', 'sqlscore_calibrate', 'PostgreSQL database name'],
            ['-sslmode', 'disable', 'SSL mode (disable, require, verify-ca, verify-full)'],
            ['-batch-size', '10', 'Schemas per batch in batch-and-drop mode'],
            ['-timeout', '5000', 'Per-query statement timeout (ms)'],
            ['-logfile', '', 'Log file path (use .gz extension for gzip compression)'],
            ['-schemas', '5000', 'Target number of schema variants'],
            ['-queries', '1000000', 'Target number of queries'],
            ['-rows', '70000', 'Base rows per table (tiered multipliers apply)'],
            ['-schema-file', '', 'Path to .SQL DDL file for custom calibration domain'],
            ['-output', 'src/scorer/weights.json', 'Output file for calculated weights'],
            ['-phase', 'all', 'Pipeline phase: init, generate, run, calculate, or all'],
        ]),

        createElement('h2', null, 'Custom Schema Import'),
        createElement('p', null,
            'You can provide your own business schema DDL to calibrate weights against your actual database structure. ',
            'This ensures the weights reflect your specific workload alongside generic patterns.',
        ),
        createElement('pre', {},
            '-- my_app_schema.sql\n' +
            'CREATE TABLE users (\n' +
            '  id SERIAL PRIMARY KEY,\n' +
            '  email VARCHAR(255) NOT NULL,\n' +
            '  created_at TIMESTAMPTZ DEFAULT now()\n' +
            ');\n' +
            'CREATE INDEX idx_users_email ON users(email);\n' +
            'CREATE TABLE orders (\n' +
            '  id SERIAL PRIMARY KEY,\n' +
            '  user_id INT NOT NULL,\n' +
            '  total NUMERIC(10,2)\n' +
            ');\n' +
            'ALTER TABLE orders ADD CONSTRAINT fk_user\n' +
            '  FOREIGN KEY (user_id) REFERENCES users(id);\n',
        ),
        createElement('p', null,
            'The tool parses the DDL, extracts tables/indexes/FKs, and feeds them through the same mutation and calibration pipeline as the generated schemas.',
        ),
    );
}

function table(rows: string[][]) {
    const header = rows[0];
    const body = rows.slice(1);
    return createElement('table', {style: 'width: 100%; border-collapse: collapse; margin: 1rem 0;'},
        createElement('thead', null,
            createElement('tr', null,
                ...header.map(h => createElement('th', {style: 'text-align: left; padding: 0.5rem; border-bottom: 2px solid #ddd; font-size: 0.9rem;'}, h)),
            ),
        ),
        createElement('tbody', null,
            ...body.map(row =>
                createElement('tr', null,
                    ...row.map((cell, i) => createElement('td', {
                        style: `padding: 0.5rem; border-bottom: 1px solid #eee; font-size: 0.9rem; ${i === 0 ? 'font-family: monospace; font-weight: 600;' : ''}`,
                    }, cell)),
                ),
            ),
        ),
    );
}
