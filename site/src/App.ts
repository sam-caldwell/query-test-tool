import {createElement, Router, Route, useHead, useState} from '@asymmetric-effort/specifyjs';
import {Overview} from './pages/Overview';
import {Scoring} from './pages/Scoring';
import {Calibration} from './pages/Calibration';
import {Installation} from './pages/Installation';
import {Usage} from './pages/Usage';
import {Architecture} from './pages/Architecture';
import {Api} from './pages/Api';

const navItems = [
    {id: 'overview', label: 'Overview'},
    {id: 'scoring', label: 'Scoring Rules'},
    {id: 'calibration', label: 'Calibration'},
    {id: 'installation', label: 'Installation'},
    {id: 'usage', label: 'Usage'},
    {id: 'architecture', label: 'Architecture'},
    {id: 'api', label: 'Library API'},
];

function NavSidebar() {
    const [selectedId, setSelectedId] = useState('overview');

    function handleClick(id: string) {
        setSelectedId(id);
        window.location.hash = '#/' + id;
    }

    return createElement('nav', {
            style: 'width:240px; min-height:100vh; background:#1a1a2e; padding:1rem 0; overflow-y:auto;'
        },
        createElement('div', {style: 'padding: 0.75rem 1.5rem; margin-bottom: 1rem;'},
            createElement('h2', {style: 'color:#fff; font-size:1.1rem; margin:0;'}, 'sqlscore'),
            createElement('p', {style: 'color:#888; font-size:0.75rem; margin:0.25rem 0 0;'}, 'Query Scoring Tool'),
        ),
        ...navItems.map(item =>
            createElement('a', {
                href: '#/' + item.id,
                onClick: (e: Event) => { e.preventDefault(); handleClick(item.id); },
                style: `display:block; padding:0.6rem 1.5rem; color:${selectedId === item.id ? '#fff' : '#aaa'}; text-decoration:none; background:${selectedId === item.id ? '#16213e' : 'transparent'}; border-left:3px solid ${selectedId === item.id ? '#4fc3f7' : 'transparent'}; font-size:0.9rem;`,
            }, item.label),
        ),
    );
}

function Layout() {
    useHead({
        title: 'sqlscore — SQL Query Scoring Tool',
        description: 'Static analysis tool that scores SQL queries for efficiency, memory/compute cost, and cognitive complexity.',
        keywords: 'sql,scoring,efficiency,query,analysis,postgresql,go',
        author: 'Sam Caldwell',
        canonical: 'https://query-test-tool.samcaldwell.net',
    });

    return createElement('div', {style: 'display:flex; min-height:100vh; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;'},
        createElement(NavSidebar, null),
        createElement('main', {style: 'flex:1; overflow-y:auto; padding: 2rem 3rem; max-width: 900px;'},
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

export function App() {
    return createElement(Router, null,
        createElement(Layout, null),
    );
}
