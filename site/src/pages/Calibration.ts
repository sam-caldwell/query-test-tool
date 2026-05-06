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
            '│  10K schemas│    │  1K rows/tbl│    │  1M queries  │    │  Runner     │\n' +
            '└─────────────┘    └─────────────┘    └─────────────┘    └──────┬──────┘\n' +
            '                                                                │\n' +
            '                                                                ▼\n' +
            '                                                        ┌─────────────┐\n' +
            '                                                        │   Paired    │\n' +
            '                                                        │ Comparison  │\n' +
            '                                                        └─────────────┘\n' +
            '                                                                │\n' +
            '                                                                ▼\n' +
            '                                                       scorer/weights.json\n',
        ),

        createElement('h2', null, 'Schema Generation'),
        createElement('p', null, '7 domain archetypes (e-commerce, blog, HR, inventory, analytics, cash accounting, accrual accounting) are mutated to produce 10,000 schema variants:'),
        createElement('ul', null,
            createElement('li', null, 'Drop individual indexes (tests non-sargable, unbounded-sort)'),
            createElement('li', null, 'Drop all indexes (full scan behavior)'),
            createElement('li', null, 'Drop foreign keys (missing-predicate)'),
            createElement('li', null, 'Widen tables with extra columns (SELECT * cost)'),
            createElement('li', null, 'Textify numeric/date columns (type coercion)'),
            createElement('li', null, 'Denormalize tables (merge child into parent)'),
            createElement('li', null, 'Add redundant cached columns'),
        ),

        createElement('h2', null, 'Data Generation'),
        createElement('ul', null,
            createElement('li', null, 'High-cardinality values with skewed (Zipfian) distributions'),
            createElement('li', null, '10% sporadic NULLs on nullable columns'),
            createElement('li', null, 'Realistic text patterns (emails, statuses, timestamps)'),
            createElement('li', null, 'FK-consistent references using modular cycling'),
        ),

        createElement('h2', null, 'Paired Query Comparison'),
        createElement('p', null, 'For each antipattern, the generator produces a "bad" query and a control:'),
        createElement('ul', null,
            createElement('li', null, 'select_star vs select_columns (same table, * vs named cols)'),
            createElement('li', null, 'non_sargable vs sargable (LOWER(col) = x vs col = x)'),
            createElement('li', null, 'unbounded_sort vs bounded_sort (ORDER BY vs ORDER BY LIMIT)'),
            createElement('li', null, 'exists_subquery vs proper_join'),
            createElement('li', null, 'distinct_join vs proper_join'),
        ),
        createElement('p', null,
            'Weight = median(cost_antipattern / cost_control), scaled to a 1–25 range. ',
            'This directly measures how much worse the antipattern is compared to the correct pattern.',
        ),

        createElement('h2', null, 'Running Calibration'),
        createElement('pre', {},
            '# Prerequisites\n' +
            'createdb sqlscore_calibrate\n\n' +
            '# Full pipeline\n' +
            './bin/calibrate -dsn "postgres:///sqlscore_calibrate?sslmode=disable"\n\n' +
            '# Include your own business schema\n' +
            './bin/calibrate -schema-file ./my_app_schema.sql\n\n' +
            '# Rebuild with new weights\n' +
            'make build\n',
        ),

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
