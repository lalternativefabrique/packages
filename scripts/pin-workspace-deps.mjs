// npm publishes "workspace:*" as written, which no registry resolves; pnpm
// rewrites it, npm does not. Run as the package's prepack, this pins each
// workspace dependency to the version of the package it names, and with
// --restore (postpack) puts the package.json back as it was.
import { existsSync, readFileSync, readdirSync, renameSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const manifest = resolve('package.json');
const backup = `${manifest}.workspace`;

if (process.argv.includes('--restore')) {
  if (existsSync(backup)) renameSync(backup, manifest);
  process.exit(0);
}

const versions = new Map();
for (const group of readdirSync(root, { withFileTypes: true })) {
  if (!group.isDirectory() || group.name.startsWith('.') || group.name === 'node_modules') continue;
  for (const dir of [join(root, group.name), ...readdirSync(join(root, group.name), { withFileTypes: true })
    .filter((d) => d.isDirectory() && d.name !== 'node_modules')
    .map((d) => join(root, group.name, d.name))]) {
    const file = join(dir, 'package.json');
    if (!existsSync(file)) continue;
    const pkg = JSON.parse(readFileSync(file, 'utf8'));
    if (pkg.name && pkg.version) versions.set(pkg.name, pkg.version);
  }
}

const raw = readFileSync(manifest, 'utf8');
const pkg = JSON.parse(raw);
let pinned = 0;
for (const field of ['dependencies', 'peerDependencies', 'optionalDependencies']) {
  for (const [name, spec] of Object.entries(pkg[field] ?? {})) {
    if (!spec.startsWith('workspace:')) continue;
    const version = versions.get(name);
    if (!version) throw new Error(`${name} is a workspace dependency no package of this repository provides`);
    const range = spec.slice('workspace:'.length);
    pkg[field][name] = range === '*' || range === '' ? version : range === '^' || range === '~' ? range + version : range;
    pinned++;
  }
}
if (pinned > 0) {
  writeFileSync(backup, raw);
  writeFileSync(manifest, JSON.stringify(pkg, null, 2) + '\n');
}
