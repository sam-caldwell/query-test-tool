import {createElement} from '@asymmetric-effort/specifyjs';
import {Overview} from './pages/Overview';
import {Scoring} from './pages/Scoring';
import {Calibration} from './pages/Calibration';
import {Installation} from './pages/Installation';
import {Usage} from './pages/Usage';
import {Architecture} from './pages/Architecture';
import {Api} from './pages/Api';

const pages: Record<string, () => ReturnType<typeof createElement>> = {
    overview: Overview,
    scoring: Scoring,
    calibration: Calibration,
    installation: Installation,
    usage: Usage,
    architecture: Architecture,
    api: Api,
};

const navItems = [
    {id: 'overview', label: 'Overview'},
    {id: 'scoring', label: 'Scoring Rules'},
    {id: 'calibration', label: 'Calibration'},
    {id: 'installation', label: 'Installation'},
    {id: 'usage', label: 'Usage'},
    {id: 'architecture', label: 'Architecture'},
    {id: 'api', label: 'Library API'},
];

export function App() {
    // Determine current page from hash
    const hash = window.location.hash.replace('#/', '') || 'overview';
    const PageComponent = pages[hash] || Overview;

    return createElement('div', {style: 'display:flex; min-height:100vh; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;'},
        createElement('nav', {style: 'width:240px; min-height:100vh; background:#1a1a2e; padding:1rem 0;'},
            createElement('div', {style: 'padding: 0.75rem 1.5rem; margin-bottom: 1rem;'},
                createElement('h2', {style: 'color:#fff; font-size:1.1rem; margin:0;'}, 'sqlscore'),
                createElement('p', {style: 'color:#888; font-size:0.75rem; margin:0.25rem 0 0;'}, 'Query Scoring Tool'),
            ),
            ...navItems.map(item =>
                createElement('a', {
                    href: '#/' + item.id,
                    style: `display:block; padding:0.6rem 1.5rem; color:${hash === item.id ? '#fff' : '#aaa'}; text-decoration:none; background:${hash === item.id ? '#16213e' : 'transparent'}; border-left:3px solid ${hash === item.id ? '#4fc3f7' : 'transparent'}; font-size:0.9rem;`,
                }, item.label),
            ),
        ),
        createElement('main', {style: 'flex:1; overflow-y:auto; padding: 2rem 3rem; max-width: 900px;'},
            createElement(PageComponent, null),
        ),
    );
}
