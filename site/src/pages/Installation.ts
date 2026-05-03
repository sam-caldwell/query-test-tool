import {createElement} from '@asymmetric-effort/specifyjs';

export function Installation() {

    return createElement('div', null,
        createElement('h1', null, 'Installation'),

        createElement('h2', null, 'From Source'),
        createElement('pre', {},
            'git clone https://github.com/sam-caldwell/query-test-tool.git\n' +
            'cd query-test-tool\n' +
            'make build      # builds to bin/\n' +
            'make install    # copies to ~/.bin/\n',
        ),

        createElement('h2', null, 'Go Install'),
        createElement('pre', {},
            'go install github.com/sqlscore/cmd/sqlscore@latest\n' +
            'go install github.com/sqlscore/cmd/calibrate@latest\n',
        ),

        createElement('h2', null, 'Requirements'),
        createElement('ul', null,
            createElement('li', null, 'Go 1.21+ (cgo required for pg_query)'),
            createElement('li', null, 'C compiler (gcc or clang)'),
            createElement('li', null, 'PostgreSQL (for calibration only)'),
        ),

        createElement('h2', null, 'Build Targets'),
        createElement('p', null, 'The Makefile supports:'),
        createElement('pre', {},
            'make clean      # Remove and recreate bin/\n' +
            'make lint       # go vet + govulncheck\n' +
            'make build      # Build using existing weights\n' +
            'make build/full # Generate fresh weights, then build\n' +
            'make test       # Unit → integration → e2e tests\n' +
            'make install    # Copy to ~/.bin/\n' +
            'make release    # Bump patch version, tag\n',
        ),

        createElement('h2', null, 'Platform Support'),
        createElement('ul', null,
            createElement('li', null, 'Linux amd64'),
            createElement('li', null, 'Linux arm64'),
            createElement('li', null, 'macOS arm64 (Apple Silicon)'),
        ),
    );
}
