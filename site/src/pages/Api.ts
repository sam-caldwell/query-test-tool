import {createElement} from '@asymmetric-effort/specifyjs';

export function Api() {

    return createElement('div', null,
        createElement('h1', null, 'Library API'),
        createElement('p', null, 'sqlscore can be used as a Go library for programmatic SQL analysis.'),

        createElement('h2', null, 'ScoreQuery'),
        createElement('pre', {},
            'import "github.com/sqlscore/scorer"\n\n' +
            'report, err := scorer.ScoreQuery("SELECT * FROM users ORDER BY name")\n' +
            'if err != nil {\n' +
            '    log.Fatal(err)\n' +
            '}\n\n' +
            'fmt.Printf("Total: %d\\n", report.TotalScore)\n' +
            'for _, f := range report.Efficiency.Findings {\n' +
            '    fmt.Printf("  [%s] %s\\n", f.Rule, f.Description)\n' +
            '}\n',
        ),

        createElement('h2', null, 'Report Structure'),
        createElement('pre', {},
            'type Report struct {\n' +
            '    SQL              string         `json:"sql"`\n' +
            '    TotalScore       int            `json:"total_score"`\n' +
            '    Efficiency       DimensionScore `json:"efficiency"`\n' +
            '    MemoryCompute    DimensionScore `json:"memory_compute"`\n' +
            '    CognitiveComplex DimensionScore `json:"cognitive_complexity"`\n' +
            '}\n\n' +
            'type DimensionScore struct {\n' +
            '    Name     string    `json:"name"`\n' +
            '    Score    int       `json:"score"`\n' +
            '    Findings []Finding `json:"findings"`\n' +
            '}\n\n' +
            'type Finding struct {\n' +
            '    Rule        string `json:"rule"`\n' +
            '    Description string `json:"description"`\n' +
            '    Penalty     int    `json:"penalty"`\n' +
            '    Category    string `json:"category"`\n' +
            '}\n',
        ),

        createElement('h2', null, 'Accessing Weights'),
        createElement('pre', {},
            'import "github.com/sqlscore/scorer"\n\n' +
            '// Get all loaded weights\n' +
            'w := scorer.Weights()\n' +
            'fmt.Printf("Version: %d\\n", w.Version)\n' +
            'fmt.Printf("select-star weight: %d\\n", w.Weights["select-star"])\n\n' +
            '// Get a single weight\n' +
            'penalty := scorer.Weight("non-sargable") // returns 12\n',
        ),

        createElement('h2', null, 'Parser Package'),
        createElement('pre', {},
            'import "github.com/sqlscore/parser"\n\n' +
            '// Parse SQL into PostgreSQL AST\n' +
            'tree, err := parser.Parse("SELECT id FROM users WHERE id = 1")\n\n' +
            '// Walk the AST\n' +
            'parser.Walk(tree.Stmts[0].Stmt, 0, func(node *pg_query.Node, depth int) bool {\n' +
            '    // process each node\n' +
            '    return true // continue walking\n' +
            '})\n',
        ),
    );
}
