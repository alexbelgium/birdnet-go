import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, waitFor } from '@testing-library/svelte';
import { renderTyped } from '../../../test/render-helpers';
import { navigation } from '$lib/stores/navigation.svelte';
import { resetBasePath, setBasePath } from '$lib/utils/urlHelpers';
import System from './System.svelte';
import systemSource from './System.svelte?raw';
import { compile } from 'svelte/compiler';

// Only subpage selection is under test; keep child views and background requests quiet.
vi.mock('$lib/desktop/components/database', () => ({ DatabaseDashboard: () => {} }));
vi.mock('$lib/desktop/features/system/TerminalPage.svelte', () => ({ default: () => {} }));
vi.mock('$lib/desktop/views/SystemInference.svelte', () => ({ default: () => {} }));
vi.mock('$lib/desktop/features/import-export/pages/ImportExportPage.svelte', () => ({
  default: () => {},
}));
vi.mock('$lib/desktop/features/system/components/MetricStrip.svelte', () => ({
  default: () => {},
}));
vi.mock('$lib/desktop/features/system/components/SystemDetailsCard.svelte', () => ({
  default: () => {},
}));
vi.mock('$lib/desktop/features/system/components/StorageCard.svelte', () => ({
  default: () => {},
}));
vi.mock('$lib/desktop/features/system/components/SystemProcessTable.svelte', () => ({
  default: () => {},
}));
vi.mock('$lib/desktop/features/system/components/RestartCard.svelte', () => ({
  default: () => {},
}));
vi.mock('$lib/utils/api', () => ({
  api: { get: vi.fn().mockResolvedValue([]) },
  ApiError: class extends Error {},
}));
vi.mock('$lib/utils/ReconnectingEventSource', () => ({
  ReconnectingEventSource: class {
    addEventListener(): void {}
    close(): void {}
  },
}));

const SYSTEM_PATH = ['', 'ui', 'system'].join('/');
const INGRESS_PREFIX = '/api/hassio_ingress/test-prefix';
const SUBPAGES = [
  ['', 'system.aria.dashboard'],
  ['database', 'system.database.dashboard.title'],
  ['terminal', 'system.sections.terminal'],
  ['inference', 'system.sections.inference'],
  ['import-export', 'system.sections.importExport'],
];

afterEach(() => {
  cleanup();
  resetBasePath();
  navigation.redirect(['', 'ui', 'dashboard'].join('/'));
  vi.restoreAllMocks();
});

describe.each(['', INGRESS_PREFIX])('System navigation with prefix %s', prefix => {
  it('shows each selected subpage and preserves the prefix in browser history', async () => {
    setBasePath(prefix);
    const pushState = vi.spyOn(window.history, 'pushState');
    navigation.navigate(`${prefix}${SYSTEM_PATH}`);
    const view = renderTyped(System);

    for (const [subpage, label] of SUBPAGES) {
      const path = subpage ? `${SYSTEM_PATH}/${subpage}` : SYSTEM_PATH;
      navigation.navigate(`${prefix}${path}?test=1#section`);
      await waitFor(() => expect(view.getByRole('region', { name: label })).toBeInTheDocument());
      expect(navigation.currentPath).toBe(path);
      expect(pushState).toHaveBeenLastCalledWith({}, '', `${prefix}${path}?test=1#section`);
    }
  });
});

// Exercise the proxy's response-body rewrite on emitted JavaScript, not just a
// prefixed input URL: the navigation store deliberately removes that prefix.
it('keeps System route matching intact when a proxy rewrites quoted UI paths', () => {
  const { js } = compile(systemSource, { generate: 'client' });
  const rewritten = js.code
    .replaceAll("'/u", `'${INGRESS_PREFIX}/u`)
    .replaceAll('"/u', `"${INGRESS_PREFIX}/u`)
    .replaceAll('`/u', `\`${INGRESS_PREFIX}/u`);
  expect(rewritten).toBe(js.code);
});
