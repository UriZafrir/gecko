// Gecko's web UI as one page for the iOS app (see ios/Gecko/BundledUI.swift).
//
// The app shows this page for every machine at the machine's own origin
// (WKWebView.loadHTMLString with the machine as base URL), so the token
// cookie, /api/ and /ws work as in a browser. WKWebView can't answer the
// page's http(s) requests itself, so everything the page needs to start is
// in it: the script, the styles and the fonts (as data: URLs). The theme
// photos come from the app through its gecko-asset: scheme.
//
// Usage: node bundle-web.mjs <web/dist> <out dir> [commit] [time]
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

const [dist = 'web/dist', out = 'ios/Gecko/Generated', commit = 'dev', time = ''] = process.argv.slice(2);
const read = (p) => readFileSync(join(dist, p), 'utf8');
if (!existsSync(join(dist, 'index.html')) || !existsSync(join(dist, 'main.js'))) {
  console.error(`bundle-web: no web UI build in ${dist}; run make web first`);
  process.exit(1);
}

const mime = { woff2: 'font/woff2', woff: 'font/woff', svg: 'image/svg+xml', png: 'image/png', jpg: 'image/jpeg' };

// Styles: fonts inline. The .woff fallbacks are for browsers without
// woff2, which WebKit on iOS 16 and later isn't.
let css = read('main.css');
css = css.replace(/,\s*url\("?\.\/assets\/[^")]+\.woff"?\)\s*format\("woff"\)/g, '');
css = css.replace(/url\("?\.\/(assets\/[^")]+)"?\)/g, (_, p) => {
  const ext = p.split('.').pop();
  const b64 = readFileSync(join(dist, p)).toString('base64');
  return `url("data:${mime[ext] || 'application/octet-stream'};base64,${b64}")`;
});
if (/url\("?\.\//.test(css)) throw new Error('bundle-web: a relative url() is left in main.css');

// The script: the theme photos from the app, and nothing that would end
// the inline <script> early.
let js = read('main.js').replaceAll('"/themes/', '"gecko-asset://ui/themes/');
js = js.replace(/<\/script/gi, '<\\/script');

let html = read('index.html');
const swap = (from, to) => {
  if (!html.includes(from)) throw new Error(`bundle-web: index.html has no ${from}`);
  html = html.replace(from, () => to);
};
// Pinching is the terminal's (its font size), not WebKit's page zoom.
swap('interactive-widget=resizes-content">', 'interactive-widget=resizes-content, maximum-scale=1, user-scalable=no">');
swap('<link rel="manifest" href="/manifest.webmanifest">', '<meta name="gecko-ui" content="ios-app">');
swap('<link rel="stylesheet" href="/main.css">', `<style>${css}</style>`);
swap('<script src="/main.js" type="module"></script>', `<script type="module">${js}</script>`);

rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, 'web'), { recursive: true });
writeFileSync(join(out, 'web', 'index.html'), html);
cpSync(join(dist, 'themes'), join(out, 'web', 'themes'), { recursive: true });
for (const f of ['icon.svg', 'icon-alert.svg']) cpSync(join(dist, f), join(out, 'web', f));
// The app's build id, the same as the web UI's and gecko's (BuildId.swift).
writeFileSync(join(out, 'build-id.json'), JSON.stringify({ commit, time }) + '\n');
mkdirSync(dirname(out), { recursive: true });
console.log(`bundle-web: ${join(out, 'web', 'index.html')} (${(html.length / 1e6).toFixed(1)} MB), build ${commit} ${time}`);
