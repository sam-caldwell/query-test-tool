import {createElement, useHead} from '@asymmetric-effort/specifyjs';

export function Scoring() {
    useHead({
        title: 'sqlscore — Scoring Rules',
        description: 'Detailed scoring rules for SQL query efficiency, memory/compute, and cognitive complexity.',
    });

    return createElement('div', null,
        createElement('h1', null, 'Scoring Rules'),
        createElement('p', null, 'Each finding adds a calibrated penalty to its dimension. The total score is the sum of all dimensions.'),

        createElement('h2', null, 'Grades'),
        table([
            ['Score', 'Grade'],
            ['0', 'Excellent'],
            ['1–10', 'Good'],
            ['11–25', 'Fair'],
            ['26–50', 'Poor'],
            ['51+', 'Critical'],
        ]),

        createElement('h2', null, 'Efficiency Rules'),
        createElement('p', null, 'Anti-patterns that prevent the optimizer from using indexes or cause full table scans.'),
        table([
            ['Rule', 'Weight', 'Description'],
            ['select-star', '1', 'SELECT * prevents index-only scans and fetches unnecessary columns'],
            ['missing-predicate', '1', 'Multiple tables in FROM without WHERE — likely missing join predicate'],
            ['correlated-subquery', '25', 'Subquery that may execute once per outer row (EXISTS, IN, ANY)'],
            ['non-sargable', '12', 'Function on column in WHERE prevents index usage (LOWER, TRIM, etc.)'],
            ['distinct-dedup', '25', 'DISTINCT with JOIN suggests join produces duplicates'],
        ]),

        createElement('h2', null, 'Memory/Compute Rules'),
        createElement('p', null, 'Operations that require materializing intermediate result sets in memory.'),
        table([
            ['Rule', 'Weight', 'Description'],
            ['unbounded-sort', '13', 'ORDER BY without LIMIT requires materializing and sorting entire result'],
            ['group-by-fanout', '25', 'GROUP BY with aggregation requires materializing groups in memory'],
            ['window-function', '1', 'Window function maintains state across partition (+1 without PARTITION BY)'],
            ['cartesian-product', '1', 'CROSS JOIN or implicit cross join produces O(n×m) rows'],
        ]),

        createElement('h2', null, 'Cognitive Complexity Rules'),
        createElement('p', null, 'Readability and reasoning cost, adapted from cyclomatic complexity.'),
        table([
            ['Rule', 'Weight', 'Description'],
            ['subquery-nesting', '1 × depth', 'Each subquery nesting level multiplies penalty'],
            ['join', '1', 'Each JOIN adds a relationship to reason about'],
            ['boolean-nesting', '8 × depth', 'Nested AND/OR expressions compound cognitive load'],
            ['cte', '1', 'Each CTE adds a named scope to understand'],
            ['case-expression', '25', 'CASE adds conditional branching logic'],
            ['set-operation', '25', 'UNION/INTERSECT/EXCEPT combines result sets'],
        ]),

        createElement('h2', null, 'Weight Derivation'),
        createElement('p', null,
            'All weights are derived empirically from EXPLAIN ANALYZE results against PostgreSQL. ',
            'The calibration tool generates paired queries (antipattern vs. control) on the same schema and measures the cost ratio. ',
            'Higher weights mean the antipattern causes proportionally more query cost.',
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
