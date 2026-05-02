import {createElement} from '@asymmetric-effort/specifyjs';

export function Overview() {

    return createElement('div', null,
        createElement('h1', null, 'sqlscore'),
        createElement('p', {style: 'font-size: 1.2rem; color: #555;'},
            'Static analysis tool that scores SQL queries across three dimensions: ',
            createElement('strong', null, 'efficiency'), ', ',
            createElement('strong', null, 'memory/compute cost'), ', and ',
            createElement('strong', null, 'cognitive complexity'), '.',
        ),
        createElement('h2', null, 'How It Works'),
        createElement('ol', null,
            createElement('li', null, 'Parse any PostgreSQL-compatible SQL via pg_query (PostgreSQL\'s own parser)'),
            createElement('li', null, 'Walk the AST with three independent scorers'),
            createElement('li', null, 'Report findings with empirically calibrated penalty weights'),
        ),
        createElement('h2', null, 'Scoring Dimensions'),
        createElement('div', {style: 'display: grid; grid-template-columns: repeat(3, 1fr); gap: 1.5rem; margin-top: 1rem;'},
            dimensionCard('Efficiency', 'Detects anti-patterns that prevent index usage or cause full scans: SELECT *, non-sargable predicates, correlated subqueries, missing join predicates.', '#e74c3c'),
            dimensionCard('Memory/Compute', 'Flags operations requiring intermediate materialization: unbounded sorts, GROUP BY fan-out, window functions, Cartesian products.', '#f39c12'),
            dimensionCard('Cognitive Complexity', 'Measures readability cost: subquery nesting depth, join count, boolean nesting, CTEs, CASE expressions, set operations.', '#3498db'),
        ),
        createElement('h2', null, 'Calibrated Weights'),
        createElement('p', null,
            'Scoring weights are derived empirically by running queries against PostgreSQL with EXPLAIN ANALYZE. ',
            'The calibration tool generates thousands of schema variants (with/without indexes, varying normalization) ',
            'and compares the cost of antipattern queries against control queries to measure real performance impact.',
        ),
        createElement('pre', {style: 'background: #1e1e1e; color: #d4d4d4; padding: 1rem; border-radius: 8px; overflow-x: auto;'},
            '$ sqlscore -q "SELECT * FROM users WHERE LOWER(email) = \'test\' ORDER BY name"\n\n' +
            'Total Score: 26 (poor)\n\n' +
            '  efficiency:             13  (2 finding(s))\n' +
            '    [+1]  select-star     SELECT * prevents index-only scans\n' +
            '    [+12] non-sargable    Function LOWER() on column prevents index usage\n' +
            '  memory_compute:         13  (1 finding(s))\n' +
            '    [+13] unbounded-sort  ORDER BY without LIMIT requires full sort\n',
        ),
    );
}

function dimensionCard(title: string, description: string, color: string) {
    return createElement('div', {style: `border-left: 4px solid ${color}; padding: 1rem; background: #f9f9f9; border-radius: 4px;`},
        createElement('h3', {style: `color: ${color}; margin-top: 0;`}, title),
        createElement('p', {style: 'margin-bottom: 0; font-size: 0.9rem;'}, description),
    );
}
