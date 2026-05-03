import {createElement} from '@asymmetric-effort/specifyjs';

export function Usage() {

    return createElement('div', null,
        createElement('h1', null, 'Usage'),

        createElement('h2', null, 'Basic Usage'),
        createElement('pre', {},
            '# Score a query directly\n' +
            'sqlscore -q "SELECT * FROM users ORDER BY name"\n\n' +
            '# From stdin\n' +
            'echo "SELECT * FROM users" | sqlscore\n\n' +
            '# From file\n' +
            'sqlscore -f query.sql\n\n' +
            '# Verbose (show individual findings)\n' +
            'sqlscore -q "SELECT * FROM users" -v\n\n' +
            '# JSON output\n' +
            'sqlscore -q "SELECT * FROM users" -format json\n',
        ),

        createElement('h2', null, 'Options'),
        createElement('pre', {},
            '  -q, -query     SQL query to score\n' +
            '  -f, -file      File containing SQL query\n' +
            '  -format        Output format: text or json (default: text)\n' +
            '  -v, -verbose   Show detailed findings\n' +
            '  -version       Show version and weights info\n',
        ),

        createElement('h2', null, 'Output Example'),
        createElement('pre', {},
            'SQL Query Score Report\n' +
            '======================\n\n' +
            'Total Score: 26 (poor)\n\n' +
            '  efficiency:             13  (2 finding(s))\n' +
            '    [+1]  select-star     SELECT * prevents index-only scans\n' +
            '    [+12] non-sargable    Function LOWER() on column prevents index usage\n' +
            '  memory_compute:         13  (1 finding(s))\n' +
            '    [+13] unbounded-sort  ORDER BY without LIMIT requires full sort\n' +
            '  cognitive_complexity:    0  (0 finding(s))\n',
        ),

        createElement('h2', null, 'JSON Output'),
        createElement('pre', {},
            '{\n' +
            '  "sql": "SELECT * FROM users",\n' +
            '  "total_score": 1,\n' +
            '  "efficiency": {\n' +
            '    "name": "efficiency",\n' +
            '    "score": 1,\n' +
            '    "findings": [{\n' +
            '      "rule": "select-star",\n' +
            '      "penalty": 1,\n' +
            '      "description": "SELECT * prevents index-only scans..."\n' +
            '    }]\n' +
            '  },\n' +
            '  ...\n' +
            '}\n',
        ),

        createElement('h2', null, 'Exit Codes'),
        createElement('ul', null,
            createElement('li', null, createElement('code', null, '0'), ' — Success'),
            createElement('li', null, createElement('code', null, '1'), ' — Error (invalid SQL, missing input, bad format)'),
        ),
    );
}
