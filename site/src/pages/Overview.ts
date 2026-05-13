import {createElement} from '@asymmetric-effort/specifyjs';

export function Overview() {

    return createElement('div', null,
        createElement('h1', null, 'sqlscore'),
        createElement('p', {className: 'lead'},
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
        createElement('div', {className: 'card-grid'},
            dimensionCard('Efficiency', 'Detects anti-patterns that prevent index usage or cause full scans: SELECT *, non-sargable predicates, correlated subqueries, missing join predicates.', 'card-red'),
            dimensionCard('Memory/Compute', 'Flags operations requiring intermediate materialization: unbounded sorts, GROUP BY fan-out, window functions, Cartesian products.', 'card-amber'),
            dimensionCard('Cognitive Complexity', 'Measures readability cost: subquery nesting depth, join count, boolean nesting, CTEs, CASE expressions, set operations.', 'card-blue'),
        ),
        createElement('h2', null, 'Calibrated Weights'),
        createElement('p', null,
            'Scoring weights are derived empirically by running 1M queries against PostgreSQL with EXPLAIN ANALYZE. ',
            'The calibration tool uses 4 merged domains (84 tables, 912 columns) to generate schema variants ',
            'with and without indexes, varying normalization, and up to 700K rows per table. ',
            'Paired comparison isolates the cost impact of each antipattern from confounding factors like table size.',
        ),
        createElement('pre', null,
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

function dimensionCard(title: string, description: string, className: string) {
    return createElement('div', {className: 'card ' + className},
        createElement('h3', null, title),
        createElement('p', null, description),
    );
}
