import {createElement, Fragment, Router, Route} from '@asymmetric-effort/specifyjs';
import {createRoot} from '@asymmetric-effort/specifyjs/dom';
import {Sidebar} from '@asymmetric-effort/specifyjs/components';
import {App} from './App';

const root = createRoot(document.getElementById('root')!);
root.render(createElement(App, null));
