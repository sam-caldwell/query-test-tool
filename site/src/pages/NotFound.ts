import {createElement} from '@asymmetric-effort/specifyjs';

export function NotFound() {
    const hash = window.location.hash.replace('#/', '') || '';

    return createElement('div', {className: 'not-found'},
        createElement('h1', null, '404 — Page Not Found'),
        createElement('p', null,
            `The page "/${hash}" does not exist.`,
        ),
        createElement('p', null,
            createElement('a', {href: '#/overview'}, 'Return to Overview'),
        ),
    );
}
