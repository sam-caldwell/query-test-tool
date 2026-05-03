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
    const hash = window.location.hash.replace('#/', '') || 'overview';
    const PageComponent = pages[hash] || Overview;

    return createElement('div', {className: 'app-layout'},
        createElement('nav', {className: 'sidebar'},
            createElement('div', {className: 'sidebar-header'},
                createElement('h2', {className: 'sidebar-title'}, 'sqlscore'),
                createElement('p', {className: 'sidebar-subtitle'}, 'Query Scoring Tool'),
            ),
            ...navItems.map(item =>
                createElement('a', {
                    href: '#/' + item.id,
                    className: 'nav-link' + (hash === item.id ? ' nav-link-active' : ''),
                }, item.label),
            ),
        ),
        createElement('main', {className: 'content'},
            createElement(PageComponent, null),
        ),
    );
}
