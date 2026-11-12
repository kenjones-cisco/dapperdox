import $ from 'jquery';
import * as bootstrap from 'bootstrap';
import hljs from 'highlight.js/lib/common';
import http from 'highlight.js/lib/languages/http';

hljs.registerLanguage('http', http);
window.$ = window.jQuery = $;
window.bootstrap = bootstrap;
window.hljs = hljs;
document.addEventListener('DOMContentLoaded', () => hljs.highlightAll());
