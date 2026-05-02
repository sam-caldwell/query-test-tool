import {createElement, Router, Route, useHead, useState, useRouter} from '@asymmetric-effort/specifyjs';
import {Sidebar} from '@asymmetric-effort/specifyjs/components';
import {Overview} from './pages/Overview';
import {Scoring} from './pages/Scoring';
import {Calibration} from './pages/Calibration';
import {Installation} from './pages/Installation';
import {Usage} from './pages/Usage';
import {Architecture} from './pages/Architecture';
import {Api} from './pages/Api';

const navItems = [
    {id: 'overview', label: 'Overview', icon: '📊'},
    {id: 'scoring', label: 'Scoring Rules', icon: '⚖️'},
    {id: 'calibration', label: 'Calibration', icon: '🔬'},
    {id: 'installation', label: 'Installation', icon: '📦'},
    {id: 'usage', label: 'Usage', icon: '💻'},
    {id: 'architecture', label: 'Architecture', icon: '🏗️'},
    {id: 'api', label: 'Library API', icon: '📚'},
];

// Inner layout component that uses useRouter (must be a child of Router)
function Layout() {
    useHead({
        title: 'sqlscore — SQL Query Scoring Tool',
        description: 'Static analysis tool that scores SQL queries for efficiency, memory/compute cost, and cognitive complexity.',
        keywords: 'sql,scoring,efficiency,query,analysis,postgresql,go',
        author: 'Sam Caldwell',
        canonical: 'https://query-test-tool.samcaldwell.net',
    });

    const [selectedId, setSelectedId] = useState('overview');
    const router = useRouter();

    function handleSelect(id: string) {
        setSelectedId(id);
        router.navigate('#/' + id);
    }

    return createElement('div', {style: 'display:flex; height:100vh; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;'},
        createElement(Sidebar, {
            items: navItems,
            selectedId,
            onSelect: handleSelect,
            width: '260px',
        }),
        createElement('main', {style: 'flex:1; overflow-y:auto; padding: 2rem 3rem;'},
            createElement(Route, {path: '/', exact: true, component: Overview}),
            createElement(Route, {path: '#/overview', component: Overview}),
            createElement(Route, {path: '#/scoring', component: Scoring}),
            createElement(Route, {path: '#/calibration', component: Calibration}),
            createElement(Route, {path: '#/installation', component: Installation}),
            createElement(Route, {path: '#/usage', component: Usage}),
            createElement(Route, {path: '#/architecture', component: Architecture}),
            createElement(Route, {path: '#/api', component: Api}),
        ),
    );
}

// App wraps Layout in Router so hooks have access to router context
export function App() {
    return createElement(Router, null,
        createElement(Layout, null),
    );
}
