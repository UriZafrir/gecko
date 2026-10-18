// A throwaway Gecko daemon for the tests: this checkout's bin/gecko and
// web/dist (make web build), a fresh GECKO_HOME and a port of its own
// (PWA_PORT, default 7910). Never the real service.
import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../..');

export default async function () {
  const bin = join(root, 'bin/gecko');
  if (!existsSync(bin)) throw new Error('no bin/gecko: run make web build first (make test-pwa does)');
  const home = mkdtempSync(join(tmpdir(), 'gecko-pwa-'));
  writeFileSync(join(home, 'settings.json'), '{"theme":"tokyo-night"}\n');
  const port = process.env.PWA_PORT || '7910';
  const d = spawn(bin, ['daemon', '--listen', `127.0.0.1:${port}`], {
    env: { ...process.env, GECKO_HOME: home, GECKO_WEB_DIR: join(root, 'web/dist'), SHELL: '/bin/sh', PS1: '$ ' },
    stdio: ['ignore', 'ignore', 'inherit'],
    detached: true,
  });
  let token = '';
  for (let i = 0; i < 100 && !token; i++) {
    await new Promise((r) => setTimeout(r, 100));
    try { token = readFileSync(join(home, 'token'), 'utf8').trim(); } catch {}
  }
  for (let i = 0; i < 50; i++) {
    try { if ((await fetch(`http://127.0.0.1:${port}/`)).ok) break; } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  if (!token) throw new Error('the daemon did not start');
  process.env.GECKO_PWA_URL = `http://127.0.0.1:${port}/?token=${token}`;
  process.env.GECKO_PWA_HOME = home;
  process.env.GECKO_PWA_PID = String(d.pid);
  d.unref();
}
