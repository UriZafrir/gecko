import { test } from 'node:test';
import assert from 'node:assert/strict';
import { keyboardHeight, KB_MIN } from './viewport.js';

test('keyboard: the screen it takes, once it is more than a bar', () => {
  assert.equal(keyboardHeight(852, 852), 0);
  assert.equal(keyboardHeight(852, 852 - 60), 0); // a browser bar coming back
  assert.equal(keyboardHeight(852, 852 - KB_MIN), KB_MIN);
  assert.equal(keyboardHeight(852, 516.4), 336);
});

test('keyboard: a zoomed page is small for that reason, not a keyboard', () => {
  assert.equal(keyboardHeight(852, 400, 2), 0);
});
