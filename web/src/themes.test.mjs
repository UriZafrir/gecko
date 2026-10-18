// Contrast of every theme: terminal palette against its background, and
// the UI tokens against the surfaces they sit on (WCAG 2 contrast ratios).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { THEMES, uiTokens, contrast, isLight, termContrast, photoBackdrop, AA } from './themes.js';

// Colored text (red ... cyan, normal and bright): WCAG's floor for large
// text and UI parts. Light terminals also get xterm's minimumContrastRatio
// of AA (checked below), which lifts anything between 3 and 4.5 at render.
const COLOR = 3;
const HUES = ['red', 'green', 'yellow', 'blue', 'magenta', 'cyan'];
const cap = (s) => s[0].toUpperCase() + s.slice(1);

// Colors meant to be dim, by convention: on a dark terminal "black" is a
// background color and bright black is the grey of comments and shell
// suggestions; on a light one the same goes for white and bright white.
// They are only required to be there at all.
const DIM = { dark: ['black', 'brightBlack'], light: ['white', 'brightWhite'] };
const DIM_MIN = 1; // may even equal the background

function failures(t) {
  const bg = t.term.background;
  const light = isLight(bg);
  const need = { foreground: AA };
  for (const h of HUES) need[h] = need['bright' + cap(h)] = COLOR;
  if (light) Object.assign(need, { black: AA, brightBlack: COLOR });
  else Object.assign(need, { white: AA, brightWhite: AA });
  for (const d of DIM[light ? 'light' : 'dark']) need[d] = DIM_MIN;
  const out = [];
  for (const [name, min] of Object.entries(need)) {
    const c = contrast(t.term[name], bg);
    if (c < min) out.push(`${name} ${t.term[name]} ${c.toFixed(2)} < ${min}`);
  }
  return out;
}

for (const t of THEMES) {
  test(`${t.id}: terminal colors are readable on its background`, () => {
    assert.deepEqual(failures(t), []);
  });

  if (t.image) {
    test(`${t.id}: terminal text reads over the photo's worst spot`, () => {
      assert.ok(t.tone, 'picture themes need a tone (see themes.js)');
      const b = photoBackdrop(t);
      assert.ok(contrast(t.term.foreground, b) >= AA, `foreground ${t.term.foreground} on ${b}: ${contrast(t.term.foreground, b).toFixed(2)}`);
    });
  }

  test(`${t.id}: cursor and selection show`, () => {
    const bg = t.term.background;
    const cursor = t.cursor || t.accent;
    assert.ok(contrast(cursor, bg) >= 3, `cursor ${cursor} on ${bg}: ${contrast(cursor, bg).toFixed(2)}`);
    // Selection is the accent at 30% over the background; text stays
    // readable on it (light terminals: xterm lifts it to AA at render).
    const sel = mixHex(bg, t.accent, 0x4d / 255);
    assert.ok(contrast(t.term.foreground, sel) >= COLOR, `text on selection ${sel}: ${contrast(t.term.foreground, sel).toFixed(2)}`);
  });

  test(`${t.id}: chrome text and icons meet WCAG AA`, () => {
    const u = uiTokens(t);
    const bad = [];
    for (const k of ['text', 'muted', 'faint', 'idle', 'accent', 'need', 'red']) {
      for (const s of ['bg', 'panel', 'raise']) {
        const c = contrast(u[k], u[s]);
        if (c < AA) bad.push(`${k} ${u[k]} on ${s} ${u[s]}: ${c.toFixed(2)}`);
      }
    }
    const oa = contrast(u['on-accent'], u.accent);
    if (oa < AA) bad.push(`on-accent ${u['on-accent']} on accent ${u.accent}: ${oa.toFixed(2)}`);
    assert.deepEqual(bad, []);
  });

  test(`${t.id}: muted and faint keep their order under text`, () => {
    const u = uiTokens(t);
    const on = (k) => contrast(u[k], u.bg);
    assert.ok(on('text') >= on('muted') && on('muted') >= on('faint'), `text ${on('text').toFixed(2)} muted ${on('muted').toFixed(2)} faint ${on('faint').toFixed(2)}`);
  });
}

test('light terminals ask xterm for AA contrast, dark ones keep their palette', () => {
  for (const t of THEMES) {
    if (isLight(t.term.background)) assert.ok(termContrast(t) >= AA, t.id);
    else assert.equal(termContrast(t), t.contrast ?? 1.1, t.id);
  }
});

test('contrast() matches WCAG reference values', () => {
  assert.equal(contrast('#000000', '#ffffff').toFixed(2), '21.00');
  assert.equal(contrast('#777777', '#ffffff').toFixed(2), '4.48');
  assert.equal(contrast('#ffffff', '#ffffff'), 1);
});

function mixHex(a, b, t) {
  const p = (c) => [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16));
  const x = p(a), y = p(b);
  return '#' + x.map((v, i) => Math.round(v + (y[i] - v) * t).toString(16).padStart(2, '0')).join('');
}
