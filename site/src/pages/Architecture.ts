import {createElement} from '@asymmetric-effort/specifyjs';

export function Architecture() {

    return createElement('div', null,
        createElement('h1', null, 'Architecture'),

        createElement('h2', null, 'Pipeline'),
        createElement('pre', {},
            'SQL string → Parser → AST → Scorers → Report\n',
        ),

        createElement('h2', null, 'Package Layout'),
        createElement('pre', {},
            'query-test-tool/\n' +
            '├── scorer/           # Scoring engine\n' +
            '│   ├── weights.json  # Embedded calibrated weights\n' +
            '│   ├── weights.go    # go:embed loader\n' +
            '│   ├── scorer.go     # ScoreQuery(), Report, types\n' +
            '│   ├── efficiency.go # EfficiencyScorer\n' +
            '│   ├── memory_compute.go\n' +
            '│   └── cognitive.go  # CognitiveScorer\n' +
            '├── parser/           # pg_query wrapper\n' +
            '│   └── parser.go     # Parse(), Walk(), Children()\n' +
            '├── calibrate/        # Weight calibration system\n' +
            '│   ├── archetype.go  # 7 domain archetypes\n' +
            '│   ├── mutation.go   # Schema degradation generators\n' +
            '│   ├── schemagen.go  # Schema family generation\n' +
            '│   ├── datagen.go    # Data population (NULLs, skew)\n' +
            '│   ├── querygen.go   # 18 query templates\n' +
            '│   ├── runner.go     # EXPLAIN execution\n' +
            '│   └── regression.go # Paired comparison weights\n' +
            '├── cmd/sqlscore/     # CLI\n' +
            '└── cmd/calibrate/    # Calibration CLI\n',
        ),

        createElement('h2', null, 'Key Design Decisions'),
        createElement('h3', null, 'Embedded Weights'),
        createElement('p', null,
            'Scoring weights are compiled into the binary via Go\'s //go:embed. No external config files needed at runtime. ',
            'The calibrate tool writes scorer/weights.json; rebuilding picks up new weights.',
        ),

        createElement('h3', null, 'Independent Scorers'),
        createElement('p', null,
            'Each dimension (efficiency, memory/compute, cognitive) walks the AST independently. ',
            'This makes each scorer testable in isolation and trivial to extend.',
        ),

        createElement('h3', null, 'PostgreSQL Parser via cgo'),
        createElement('p', null,
            'pg_query_go wraps libpg_query — PostgreSQL\'s actual parser compiled as a C library. ',
            'This means any SQL that PostgreSQL accepts can be parsed and scored. No grammar maintenance needed.',
        ),

        createElement('h3', null, 'Paired Comparison for Calibration'),
        createElement('p', null,
            'Instead of regression on absolute costs, weights are derived by directly comparing antipattern queries against control queries on the same schema. ',
            'This isolates the cost impact of each antipattern from confounding factors like table size.',
        ),

        createElement('h2', null, 'Test Coverage'),
        createElement('ul', null,
            createElement('li', null, 'Parser: 100%'),
            createElement('li', null, 'Scorer: 98.4%'),
            createElement('li', null, '17 end-to-end tests verify calibrated weights via binary execution'),
            createElement('li', null, 'Race-free (tested with -race flag)'),
        ),
    );
}
