import fs from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const files = [
  'index.js',
  ...fs.readdirSync(path.join(root, 'core', 'casc')).map(f => `core/casc/${f}`),
  ...fs.readdirSync(path.join(root, 'core', 'db')).filter(f => f.endsWith('.js')).map(f => `core/db/${f}`),
  ...fs.readdirSync(path.join(root, 'core', 'db', 'caches')).map(f => `core/db/caches/${f}`),
];

const capabilities = files.map(file => ({
  file,
  exports: fs.readFileSync(path.join(root, file), 'utf8')
    .split(/\r?\n/)
    .filter(line => line.includes('module.exports') || line.match(/^\s*(async\s+)?[A-Za-z0-9_]+\(/))
    .slice(0, 80),
}));

console.log(JSON.stringify({ ok: true, capabilities }, null, 2));
