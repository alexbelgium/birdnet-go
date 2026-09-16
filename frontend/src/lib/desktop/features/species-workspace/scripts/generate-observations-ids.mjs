#!/usr/bin/env node
/**
 * Generates data/observationsIds.json: scientific name → observation.org species id,
 * used to link a species to https://observations.be/species/{id}/.
 *
 * Inputs: the BirdNET V2.4 label file (all species BirdNET-Go can detect) plus the
 * European bat species BattyBirdNET classifies, and any extra label files given on
 * the command line ("Scientific name_Common name" per line, or one name per line).
 * Only exact scientific-name matches are kept; unmatched names use the site's
 * search page at runtime.
 *
 * Usage (from frontend/):
 *   node src/lib/desktop/features/species-workspace/scripts/generate-observations-ids.mjs [extra-labels.txt ...]
 */
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '../../../../../../..');
const BIRDNET_LABELS = path.join(
  repoRoot,
  'internal/classifier/data/labels/V2.4/BirdNET_GLOBAL_6K_V2.4_Labels_en_us.txt'
);
const OUTPUT = path.join(here, '../data/observationsIds.json');
const API = 'https://observation.org/api/v1/species/search/?q=';
const CONCURRENCY = 4;
const PAUSE_MS = 150;

const EUROPEAN_BATS = [
  'Barbastella barbastellus',
  'Eptesicus nilssonii',
  'Eptesicus serotinus',
  'Hypsugo savii',
  'Miniopterus schreibersii',
  'Myotis alcathoe',
  'Myotis bechsteinii',
  'Myotis blythii',
  'Myotis brandtii',
  'Myotis dasycneme',
  'Myotis daubentonii',
  'Myotis emarginatus',
  'Myotis myotis',
  'Myotis mystacinus',
  'Myotis nattereri',
  'Nyctalus lasiopterus',
  'Nyctalus leisleri',
  'Nyctalus noctula',
  'Pipistrellus kuhlii',
  'Pipistrellus nathusii',
  'Pipistrellus pipistrellus',
  'Pipistrellus pygmaeus',
  'Plecotus auritus',
  'Plecotus austriacus',
  'Rhinolophus ferrumequinum',
  'Rhinolophus hipposideros',
  'Tadarida teniotis',
  'Vespertilio murinus',
];

async function readNames(file) {
  const text = await readFile(file, 'utf8');
  return text
    .split('\n')
    .map(line => line.split('_')[0].trim())
    .filter(name => /^[A-Z][a-z-]+ [a-z-]+( [a-z-]+)?$/.test(name));
}

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

async function lookup(name) {
  for (let attempt = 0; attempt < 4; attempt++) {
    const res = await fetch(API + encodeURIComponent(name), {
      headers: { Accept: 'application/json' },
    });
    if (res.status === 429 || res.status >= 500) {
      await sleep(2000 * (attempt + 1));
      continue;
    }
    if (!res.ok) return undefined;
    const data = await res.json();
    const match = (data.results ?? []).find(r => r.scientific_name === name);
    return match?.id;
  }
  return undefined;
}

const names = new Set([...(await readNames(BIRDNET_LABELS)), ...EUROPEAN_BATS]);
for (const extra of globalThis.process.argv.slice(2))
  for (const n of await readNames(extra)) names.add(n);

const queue = [...names].sort();
const ids = {};
let done = 0;
await Promise.all(
  Array.from({ length: CONCURRENCY }, async () => {
    for (let name = queue.shift(); name !== undefined; name = queue.shift()) {
      const id = await lookup(name);
      if (typeof id === 'number') ids[name] = id;
      // eslint-disable-next-line no-console -- CLI progress
      if (++done % 250 === 0) console.log(`${done}/${names.size}`);
      await sleep(PAUSE_MS);
    }
  })
);

const sorted = Object.fromEntries(Object.entries(ids).sort(([a], [b]) => a.localeCompare(b)));
await writeFile(OUTPUT, JSON.stringify(sorted) + '\n');
// eslint-disable-next-line no-console -- CLI summary
console.log(`wrote ${Object.keys(sorted).length} of ${names.size} species to ${OUTPUT}`);
