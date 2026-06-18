#!/usr/bin/env python3
"""
Fix missing species translations in BirdNET-Go speciesdict data files.

Usage:
    python3 fix_translations.py [--dry-run]

Run from the directory containing the .json.gz files, or set DATA_DIR below.
Requires network access to en.wikipedia.org.

After running, delete this script before committing.
"""

import gzip
import json
import sys
import time
import urllib.parse
import urllib.request
import os

# --- CONFIGURATION -----------------------------------------------------------

DATA_DIR = os.path.dirname(os.path.abspath(__file__))

LANGS = ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it',
         'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv']

# Wikipedia uses 'no' for Norwegian Bokmål, we use 'nb'
WIKI_LANG_MAP = {'nb': 'no'}

DRY_RUN = '--dry-run' in sys.argv

# --- FULL LIST OF GAPS -------------------------------------------------------
# Generated from the data files. Each key is a Latin name; value is the list
# of language codes where the entry is currently untranslated (key == value).
# Species missing in ALL languages are excluded (no established vernacular name
# exists). Hybrids and non-species entries are skipped automatically below.

GAPS = {
    'Acanthoventris drewseni':     ['cs', 'da', 'de', 'en', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Antilophia galeata':          ['it'],
    'Callithrix aurita × penicillata': ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pt', 'sk', 'sv'],
    'Charadrius collaris':         ['it'],
    'Charadrius falklandicus':     ['it'],
    'Charadrius javanicus':        ['it'],
    'Charadrius modestus':         ['it'],
    'Charadrius peronii':          ['it'],
    'Charadrius veredus':          ['it'],
    'Charadrius wilsonia':         ['it'],
    'Cicada barbara':              ['cs', 'da', 'de', 'en', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Cicadetta brevipennis':       ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Cicadetta cantilatrix':       ['da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Cicadetta petryi':            ['da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Cranioleuca gutturata':       ['hu', 'it'],
    'Cyornis concretus':           ['hu'],
    'Dimissalna dimissa':          ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Euryphara contentei':         ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Guyalna bonaerensis':         ['cs', 'da', 'de', 'en', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Herpsilochmus sellowi':       ['hu', 'it'],
    'Hilaphura varipes':           ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Human non-vocal':             ['en'],
    'Human vocal':                 ['en'],
    'Human whistle':               ['en'],
    'Hyalinobatrachium kawense':   ['cs', 'da', 'de', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Ixobrychus minutus':          ['it'],
    'Laterallus xenopterus':       ['it'],
    'Melidectes torquatus':        ['cs'],
    'Mirafra affinis':             ['it'],
    'Myotis dasycneme':            ['es', 'fr', 'it', 'sv'],
    'Myotis daubentonii':          ['es', 'fr', 'it', 'sv'],
    'Myotis emarginatus':          ['es', 'fr', 'it'],
    'Myotis myotis':               ['es', 'fr', 'it', 'sv'],
    'Myotis mystacinus':           ['es', 'fr', 'it', 'sv'],
    'Myotis nattereri':            ['es', 'fr', 'it', 'sv'],
    'Nyctalus lasiopterus':        ['es', 'fr', 'it'],
    'Nyctalus leisleri':           ['es', 'fr', 'it', 'sv'],
    'Nyctalus noctula':            ['es', 'fr', 'it', 'sv'],
    'Odontophrynus asper':         ['cs', 'da', 'de', 'en', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Oligoglena tibialis':         ['da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sv'],
    'Papurana elberti':            ['da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk'],
    'Phrynobatrachus stewartae':   ['da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Phyllomyias cinereiceps':     ['hu', 'it'],
    'Phyllomyias nigrocapillus':   ['hu', 'it'],
    'Phyllomyias uropygialis':     ['hu', 'it'],
    'Pipistrellus kuhlii':         ['es', 'fr', 'it', 'sv'],
    'Pipistrellus maderensis':     ['es', 'fr', 'it'],
    'Pipistrellus nathusii':       ['es', 'fr', 'it', 'sv'],
    'Pipistrellus pipistrellus':   ['es', 'fr', 'it', 'sv'],
    'Pipistrellus pygmaeus':       ['es', 'fr', 'it', 'sv'],
    'Platycleis sabulosa':         ['cs', 'da', 'de', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Plecotus auritus':            ['sv'],
    'Plecotus spec.':              ['es', 'fr', 'it'],
    'Power tools':                 ['en'],
    'Pseudopaludicola pocoto':     ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Rhacocleis annulata':         ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Rhinolophus blasii':          ['es', 'fr', 'it'],
    'Rhinolophus ferrumequinum':   ['es', 'fr', 'it'],
    'Rhinolophus hipposideros':    ['es', 'fr', 'it'],
    'Romalea picticornis':         ['cs', 'da', 'de', 'en', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Tacua speciosa':              ['cs', 'da', 'de', 'en', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Tadarida teniotis':           ['es', 'fr', 'it'],
    'Tettigettalna argentata':     ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Tettigettalna estrellae':     ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Tettigettalna josei':         ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Tettigettalna mariae':        ['cs', 'da', 'de', 'en', 'es', 'fi', 'fr', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Tettigettula pygmea':         ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Thyreonotus corsicus':        ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Tibicina garricola':          ['cs', 'da', 'de', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Tibicina quadrisignata':      ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'sk', 'sv'],
    'Tibicina steveni':            ['cs', 'da', 'en', 'es', 'fi', 'hu', 'it', 'lv', 'nb', 'nl', 'pl', 'pt', 'sk', 'sv'],
    'Vespertilio murinus':         ['es', 'fr', 'it', 'sv'],
}

# --- MANUAL OVERRIDES --------------------------------------------------------
# For entries where Wikipedia won't resolve correctly:
# - "Plecotus spec." is a genus-level placeholder, not a real species page
# - Non-species sound-category entries that happen to have some translations

MANUAL = {
    'Plecotus spec.': {
        'fr': 'Oreillard sp.',
        'es': 'Orejudo sp.',
        'it': 'Orecchione sp.',
    },
    # "Human vocal/non-vocal/whistle" and "Power tools" have translations in
    # some languages already; keeping Latin-name fallback for English is fine.
    # Set to empty dict to leave them unchanged.
    'Human vocal':     {},
    'Human non-vocal': {},
    'Human whistle':   {},
    'Power tools':     {},
}

# Entries to skip entirely (hybrids — no Wikipedia article)
SKIP = {s for s in GAPS if '×' in s or (' x ' in s.lower() and s.count(' ') >= 2)}

# --- HELPERS -----------------------------------------------------------------

def load_all():
    data = {}
    for lang in LANGS:
        path = os.path.join(DATA_DIR, f'{lang}.json.gz')
        with gzip.open(path, 'rt', encoding='utf-8') as f:
            data[lang] = json.load(f)
    return data


def save_lang(lang, entries):
    path = os.path.join(DATA_DIR, f'{lang}.json.gz')
    with gzip.open(path, 'wt', encoding='utf-8', compresslevel=9) as f:
        json.dump(entries, f, ensure_ascii=False, separators=(',', ':'))


def wiki_translations(latin_name, target_langs):
    """
    Return {lang_code: common_name} from Wikipedia interlanguage links.
    Skips results where the title equals the Latin name (still untranslated
    on Wikipedia).
    """
    wiki_langs = [WIKI_LANG_MAP.get(l, l) for l in target_langs]
    encoded = urllib.parse.quote(latin_name.replace(' ', '_'))
    url = (
        'https://en.wikipedia.org/w/api.php'
        f'?action=query&titles={encoded}&prop=langlinks'
        f'&lllang={"||".join(wiki_langs)}&format=json&lllimit=500'
    )
    try:
        req = urllib.request.Request(
            url, headers={'User-Agent': 'birdnet-go-translation-fix/1.0 (github.com/alexbelgium/birdnet-go)'}
        )
        with urllib.request.urlopen(req, timeout=15) as resp:
            result = json.loads(resp.read())
    except Exception as e:
        print(f'    [wiki error] {e}')
        return {}

    pages = result.get('query', {}).get('pages', {})
    if not pages:
        return {}
    page = next(iter(pages.values()))
    if 'missing' in page:
        return {}

    out = {}
    for link in page.get('langlinks', []):
        wiki_lang = link['lang']
        title = link['*'].strip()
        our_lang = next((k for k, v in WIKI_LANG_MAP.items() if v == wiki_lang), wiki_lang)
        # Reject if the title IS just the Latin name
        if title and title.lower() != latin_name.lower():
            out[our_lang] = title
    return out


# --- MAIN --------------------------------------------------------------------

def main():
    print(f'Loading data files from {DATA_DIR} ...')
    data = load_all()

    updates = {lang: {} for lang in LANGS}
    total_species = len(GAPS)

    for i, (species, missing_langs) in enumerate(sorted(GAPS.items()), 1):
        prefix = f'[{i:2d}/{total_species}] {species}'

        # Skip hybrids
        if species in SKIP:
            print(f'{prefix}  →  SKIP (hybrid)')
            continue

        # Apply manual overrides
        if species in MANUAL:
            overrides = MANUAL[species]
            if not overrides:
                print(f'{prefix}  →  SKIP (non-species, no translation needed)')
                continue
            for lang, name in overrides.items():
                if lang in missing_langs:
                    updates[lang][species] = name
            found = [f'{l}={v!r}' for l, v in overrides.items() if l in missing_langs]
            print(f'{prefix}  →  manual: {", ".join(found)}')
            continue

        # Fetch from Wikipedia
        print(f'{prefix}  →  querying Wikipedia...', end=' ', flush=True)
        translations = wiki_translations(species, missing_langs)
        found = []
        for lang in missing_langs:
            if lang in translations:
                updates[lang][species] = translations[lang]
                found.append(f'{lang}={translations[lang]!r}')
        print(', '.join(found) if found else 'not found')
        time.sleep(0.15)  # be polite

    # Summary
    total_fixes = sum(len(v) for v in updates.values())
    changed_langs = [l for l in LANGS if updates[l]]
    print(f'\n{"[DRY RUN] " if DRY_RUN else ""}Fixes found: {total_fixes} entries across {changed_langs}')

    if DRY_RUN:
        for lang in changed_langs:
            print(f'\n  {lang}:')
            for sp, name in sorted(updates[lang].items()):
                print(f'    {sp!r:50s} -> {name!r}')
        print('\nDry run complete — no files written.')
        return

    # Write updated files
    for lang in changed_langs:
        for species, name in updates[lang].items():
            data[lang][species] = name
        save_lang(lang, data[lang])
        print(f'  Saved {lang}.json.gz  ({len(updates[lang])} changes)')

    print('\nDone. Remember to delete this script before committing.')


if __name__ == '__main__':
    main()
