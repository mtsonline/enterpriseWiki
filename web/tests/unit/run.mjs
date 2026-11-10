// Bundles the unit tests (TypeScript) with esbuild and runs them with node --test.
import * as esbuild from 'esbuild';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const out = fs.mkdtempSync(path.join(os.tmpdir(), 'ewiki-unit-'));
const files = fs.readdirSync(here).filter((f) => f.endsWith('.test.ts'));
for (const f of files) {
  await esbuild.build({ entryPoints: [path.join(here, f)], bundle: true, platform: 'node', format: 'esm', outfile: path.join(out, f.replace(/\.ts$/, '.mjs')), logLevel: 'error' });
}
const r = spawnSync(process.execPath, ['--test', ...files.map((f) => path.join(out, f.replace(/\.ts$/, '.mjs')))], { stdio: 'inherit' });
fs.rmSync(out, { recursive: true, force: true });
process.exit(r.status ?? 1);
