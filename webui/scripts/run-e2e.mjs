import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:3000';
const base = new URL(baseURL);
const host = base.hostname || '127.0.0.1';
const port = base.port || '3000';
const skipServer = process.env.PLAYWRIGHT_SKIP_SERVER === '1';
const playwrightBin = join(
  root,
  'node_modules',
  '.bin',
  process.platform === 'win32' ? 'playwright.cmd' : 'playwright',
);

let server;

async function isReady() {
  try {
    const response = await fetch(new URL('/login', baseURL), { signal: AbortSignal.timeout(2000) });
    return response.ok;
  } catch {
    return false;
  }
}

async function waitForServer() {
  for (let i = 0; i < 60; i += 1) {
    if (await isReady()) return;
    await delay(500);
  }
  throw new Error(`Vite dev server did not become ready at ${baseURL}`);
}

function startServer() {
  return spawn(
    process.execPath,
    ['./node_modules/vite/bin/vite.js', '--host', host, '--port', port, '--strictPort'],
    {
      cwd: root,
      stdio: 'inherit',
      windowsHide: true,
    },
  );
}

function runPlaywright() {
  return new Promise((resolve) => {
    const child = spawn(playwrightBin, ['test', ...process.argv.slice(2)], {
      cwd: root,
      env: { ...process.env, PLAYWRIGHT_BASE_URL: baseURL },
      shell: process.platform === 'win32',
      stdio: 'inherit',
      windowsHide: true,
    });
    child.on('exit', (code) => resolve(code ?? 1));
  });
}

try {
  if (!skipServer && !(await isReady())) {
    server = startServer();
    await waitForServer();
  }

  const exitCode = await runPlaywright();
  process.exitCode = exitCode;
} finally {
  if (server && !server.killed) {
    server.kill();
  }
}
