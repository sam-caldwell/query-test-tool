import {createElement} from '@asymmetric-effort/specifyjs';
import {createRoot} from '@asymmetric-effort/specifyjs/dom';
import {App} from './App';
import './styles.css';

const container = document.getElementById('root')!;
const root = createRoot(container);

function render() {
    root.render(createElement(App, null));
}

render();
window.addEventListener('hashchange', render);
