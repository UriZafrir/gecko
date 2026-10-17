// Phone keyboards (IMEs) edit text in place: voice typing grows a phrase
// word by word, replaces an earlier word, adds a space or a comma, takes a
// word back after it was committed. A terminal only takes keys, so Gecko
// keeps what the keyboard has written since the last real key (Enter, an
// arrow, the extra keys) and sends each change as the keys that make it:
// backspaces for what changed, then the new text. Live, so the line in the
// shell or the agent follows what you say.

const chars = (s) => Array.from(s); // by code point: an emoji is one character to a shell

// From what was sent (prev) to what the keyboard shows now (next): how many
// characters to take back and what to type after.
export function imeDiff(prev, next) {
  const a = chars(prev), b = chars(next);
  let p = 0;
  while (p < a.length && p < b.length && a[p] === b[p]) p++;
  return { del: a.length - p, text: b.slice(p).join('') };
}

// The bytes for a diff: DEL (what Backspace sends) per character, then the text.
export function imeBytes({ del, text }) {
  return '\x7f'.repeat(del) + text;
}

// Android only: other phones' keyboards behave differently, desktops go
// through xterm's own handling.
export const imeMirror = /Android/.test(globalThis.navigator?.userAgent || '');

// Takes over xterm's textarea for keyboard edits. send(bytes) gets the
// keys; returns reset(), for when the line was used up some other way.
// Listens on host (an ancestor) while events go down to the textarea, so
// before xterm's own listeners on it.
export function mirrorIme(ta, send, host = ta.parentElement) {
  let sent = '';
  // A real key is down: xterm sends it, and what it does to the textarea
  // (a space typed, a character deleted) is already in the shell.
  let keyed = false;
  const sync = () => {
    const v = ta.value;
    if (v === sent) return;
    const bytes = imeBytes(imeDiff(sent, v));
    sent = v;
    if (bytes) send(bytes);
  };
  const reset = () => {
    sync();
    sent = '';
    if (ta.value) ta.value = '';
  };
  // Edits: ours alone (xterm would send them again, a different way).
  for (const type of ['input', 'compositionstart', 'compositionupdate', 'compositionend', 'beforeinput']) {
    host.addEventListener(type, (ev) => {
      if (ev.target !== ta) return;
      ev.stopImmediatePropagation();
      if (keyed && type === 'input') { sent = ta.value; return; }
      if (type === 'input' || type === 'compositionend') sync();
    }, true);
  }
  host.addEventListener('keydown', (ev) => {
    if (ev.target !== ta) return;
    // The keyboard announcing an edit: the input event brings it.
    if (ev.keyCode === 229 || ev.key === 'Unidentified' || ev.key === 'Process') {
      ev.stopImmediatePropagation();
      return;
    }
    // A real key (Enter, Backspace on an empty line, arrows, a hardware
    // keyboard): what was written is the shell's now; xterm sends the key.
    reset();
    keyed = true;
    setTimeout(() => { keyed = false; }, 50);
  }, true);
  host.addEventListener('keyup', (ev) => { if (ev.target === ta) keyed = false; }, true);
  ta.addEventListener('focus', () => { sent = ''; ta.value = ''; });
  return reset;
}
