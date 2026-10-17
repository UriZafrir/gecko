import { test } from 'node:test';
import assert from 'node:assert/strict';
import { imeDiff, imeBytes } from './ime.js';

// What a shell's line holds after these bytes (DEL takes a character back).
const line = (bytes) => {
  const out = [];
  for (const c of Array.from(bytes)) c === '\x7f' ? out.pop() : out.push(c);
  return out.join('');
};

const play = (states) => {
  let sent = '', bytes = '';
  for (const s of states) { bytes += imeBytes(imeDiff(sent, s)); sent = s; }
  return bytes;
};

test('ime: voice partials grow the line, once', () => {
  const b = play(['hello', 'hello world', 'hello world this', 'hello world this is a test']);
  assert.equal(b, 'hello world this is a test');
});

test('ime: a word replaced while composing', () => {
  const b = play(['I want to by', 'I want to buy', 'I want to buy milk']);
  assert.equal(b, 'I want to by\x7fuy milk');
  assert.equal(line(b), 'I want to buy milk');
});

test('ime: spoken comma takes its space back, corrections after the commit', () => {
  assert.equal(line(play(['first', 'first ', 'first', 'first,', 'first, second'])), 'first, second');
  assert.equal(line(play(['meet me their', 'meet me ', 'meet me there'])), 'meet me there');
  assert.equal(line(play(['send the repot today', 'send the report today'])), 'send the report today');
});

test('ime: an emoji is one character', () => {
  const b = play(['looks good 👍', 'looks good 👌']);
  assert.equal(b, 'looks good 👍\x7f👌');
  assert.deepEqual(imeDiff('a👍', 'a'), { del: 1, text: '' });
});
