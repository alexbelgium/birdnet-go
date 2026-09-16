/**
 * Column registry for the species workspace table. Ids must match the server
 * allowlist in internal/conf/species_workspace.go. Every column names the data
 * group it needs, so a hidden column never requests its data.
 */
import type { MembershipKind, WorkspaceLayout } from './types';

/** Independently loaded data groups. `inventory` is tier 0; `best` loads per visible row. */
export type DataGroup = 'inventory' | 'stats' | 'memberships' | 'range' | 'best';

export type ColumnId =
  | 'species'
  | 'count'
  | 'maxConfidence'
  | 'firstSeen'
  | 'lastSeen'
  | 'range'
  | 'verification'
  | 'excluded'
  | 'included'
  | 'confirmed'
  | 'bestRecording'
  | 'actions';

export interface ColumnDef {
  id: ColumnId;
  /** i18n key of the header label. */
  labelKey: string;
  group: DataGroup;
  sortable: boolean;
  /** Mandatory columns cannot be hidden. */
  mandatory: boolean;
  visibleByDefault: boolean;
  /** Membership list behind a membership column. */
  membership?: MembershipKind;
}

const col = (
  id: ColumnId,
  group: DataGroup,
  opts: Partial<Pick<ColumnDef, 'sortable' | 'mandatory' | 'visibleByDefault' | 'membership'>> = {}
): ColumnDef => ({
  id,
  labelKey: `speciesWorkspace.columns.${id}`,
  group,
  sortable: opts.sortable ?? true,
  mandatory: opts.mandatory ?? false,
  visibleByDefault: opts.visibleByDefault ?? false,
  membership: opts.membership,
});

export const COLUMNS: readonly ColumnDef[] = [
  col('species', 'inventory', { mandatory: true, visibleByDefault: true }),
  col('count', 'inventory', { visibleByDefault: true }),
  col('maxConfidence', 'stats', { visibleByDefault: true }),
  col('firstSeen', 'inventory'),
  col('lastSeen', 'inventory', { visibleByDefault: true }),
  col('range', 'range'),
  col('verification', 'stats', { visibleByDefault: true }),
  col('excluded', 'memberships', { membership: 'excluded' }),
  col('included', 'memberships', { membership: 'included' }),
  col('confirmed', 'memberships', { membership: 'confirmed', visibleByDefault: true }),
  // Best recordings load only for visible rows, so a global sort would be partial.
  col('bestRecording', 'best', { sortable: false, visibleByDefault: true }),
  col('actions', 'memberships', { sortable: false, mandatory: true, visibleByDefault: true }),
];

const byId = new Map<string, ColumnDef>(COLUMNS.map(c => [c.id, c]));

export function getColumn(id: string): ColumnDef | undefined {
  return byId.get(id);
}

export const DEFAULT_LAYOUT: WorkspaceLayout = {
  columns: COLUMNS.map(c => ({ id: c.id, visible: c.visibleByDefault })),
  sort: { column: 'count', direction: 'desc' },
};

/** Mirror of the server's NormalizeSpeciesWorkspaceLayout; also validates cached layouts. */
export function normalizeLayout(input: unknown): WorkspaceLayout {
  const out: WorkspaceLayout = { columns: [], sort: { ...DEFAULT_LAYOUT.sort } };
  // Cached or server data is untrusted: every field is checked before use.
  const source = (input ?? {}) as {
    columns?: unknown;
    sort?: { column?: unknown; direction?: unknown };
  };
  const seen = new Set<string>();
  const entries: unknown[] = Array.isArray(source.columns) ? source.columns : [];
  for (const raw of entries) {
    const entry = (raw ?? {}) as { id?: unknown; visible?: unknown };
    const def = typeof entry.id === 'string' ? byId.get(entry.id) : undefined;
    if (!def || seen.has(def.id)) continue;
    seen.add(def.id);
    out.columns.push({ id: def.id, visible: entry.visible === true || def.mandatory });
  }
  for (const def of COLUMNS) {
    if (!seen.has(def.id)) out.columns.push({ id: def.id, visible: def.visibleByDefault });
  }
  const column = source.sort?.column;
  if (typeof column === 'string' && byId.get(column)?.sortable) out.sort.column = column;
  const direction = source.sort?.direction;
  if (direction === 'asc' || direction === 'desc') out.sort.direction = direction;
  return out;
}

/** Visible column definitions in layout order. */
export function visibleColumns(layout: WorkspaceLayout): ColumnDef[] {
  return layout.columns.flatMap(c => {
    const def = byId.get(c.id);
    return c.visible && def ? [def] : [];
  });
}

/** Data groups required by the visible columns. */
export function neededGroups(layout: WorkspaceLayout): Set<DataGroup> {
  return new Set(visibleColumns(layout).map(c => c.group));
}
