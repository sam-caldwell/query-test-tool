import {createElement, useHead} from '@asymmetric-effort/specifyjs';

export function Calibration() {
    useHead({
        title: 'sqlscore — Weight Calibration',
        description: 'How sqlscore derives scoring weights empirically from EXPLAIN ANALYZE.',
    });

    return createElement('div', null,
        createElement('h1', null, 'Weight Calibration'),
        createElement('p', null,
            'Scoring weights are not guessed — they are derived from real PostgreSQL query execution costs. ',
            'The calibration pipeline measures how much each antipattern actually costs by comparing queries with and without the pattern.',
        ),

        createElement('h2', null, 'Pipeline'),
        createElement('pre', {style: 'background: #f5f5f5; padding: 1rem; border-radius: 8px; font-size: 0.85rem;'},
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
        createElement('p', null, '5 domain archetypes (e-commerce, blog, HR, inventory, analytics) are mutated to produce 10,000 schema variants:'),
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
        createElement('pre', {style: 'background: #1e1e1e; color: #d4d4d4; padding: 1rem; border-radius: 8px; overflow-x: auto;'},
            '# Prerequisites\n' +
            'createdb sqlscore_calibrate\n\n' +
            '# Full pipeline\n' +
            './bin/calibrate -dsn "postgres:///sqlscore_calibrate?sslmode=disable"\n\n' +
            '# Rebuild with new weights\n' +
            'make build\n',
        ),
    );
}
