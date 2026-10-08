import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const input = new URL('../dist/index.html', import.meta.url);
const output = new URL('../../internal/multiuser/panel/management.html', import.meta.url);
const html = await readFile(input);
await writeFile(output, html);
console.log(`Bundled official management frontend: ${fileURLToPath(output)}`);
