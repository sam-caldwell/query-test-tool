import {createElement} from '@asymmetric-effort/specifyjs';
import {createRoot} from '@asymmetric-effort/specifyjs/dom';
import {App} from './App';

const root = createRoot(document.getElementById('root')!);
root.render(createElement(App, null));
