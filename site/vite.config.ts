import {defineConfig} from 'vite';
import {specifyJsSeoPlugin} from '@asymmetric-effort/specifyjs/build';

export default defineConfig({
    base: '/',
    plugins: [
        specifyJsSeoPlugin({
            siteUrl: 'https://query-test-tool.samcaldwell.net',
            title: 'sqlscore — SQL Query Scoring Tool',
            description: 'Static analysis tool that scores SQL queries for efficiency, memory/compute cost, and cognitive complexity. Includes empirical weight calibration via EXPLAIN ANALYZE.',
            author: 'Sam Caldwell',
            license: 'MIT',
            repository: 'https://github.com/sam-caldwell/query-test-tool',
            npmPackage: '@sam-caldwell/query-test-tool',
            routes: [
                '/',
                '#/overview',
                '#/scoring',
                '#/calibration',
                '#/installation',
                '#/usage',
                '#/architecture',
                '#/api',
            ],
            robotsRules: [
                'User-agent: *',
                'Allow: /',
            ],
            jsonLd: {
                '@context': 'https://schema.org',
                '@type': 'SoftwareApplication',
                name: 'sqlscore',
                description: 'SQL query scoring tool with empirical weight calibration',
                applicationCategory: 'DeveloperApplication',
                operatingSystem: 'Linux, macOS',
                offers: {
                    '@type': 'Offer',
                    price: '0',
                    priceCurrency: 'USD',
                },
                author: {
                    '@type': 'Person',
                    name: 'Sam Caldwell',
                },
            },
        }),
    ],
    build: {
        outDir: 'dist',
        emptyOutDir: true,
    },
});
