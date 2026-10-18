import { rmSync } from 'node:fs';

export default async function () {
  try { process.kill(-Number(process.env.GECKO_PWA_PID), 'SIGTERM'); } catch {}
  try { process.kill(Number(process.env.GECKO_PWA_PID), 'SIGTERM'); } catch {}
  if (process.env.GECKO_PWA_HOME) rmSync(process.env.GECKO_PWA_HOME, { recursive: true, force: true });
}
