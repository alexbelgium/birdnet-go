/** URL state of the species page: page, sort and the locked-only filter. */
import type { WorkspaceRecording } from '../api';
import type { WorkspaceSort } from '../types';

export const DEFAULT_RECORDING_SORT: WorkspaceSort = 'confidence_desc';
const SORTS: readonly WorkspaceSort[] = [
  'confidence_desc',
  'confidence_asc',
  'date_desc',
  'date_asc',
];

export interface DetailQuery {
  page: number;
  sort: WorkspaceSort;
  locked: boolean;
}

export function parseDetailQuery(search: string): DetailQuery {
  const params = new URLSearchParams(search);
  const page = Number.parseInt(params.get('page') ?? '1', 10);
  const sort = params.get('sort') as WorkspaceSort | null;
  return {
    page: Number.isFinite(page) && page > 0 ? page : 1,
    sort: sort && SORTS.includes(sort) ? sort : DEFAULT_RECORDING_SORT,
    locked: params.get('locked') === 'true',
  };
}

/** Query parameters for a detail state; the sort is always explicit so a reload keeps it. */
export function detailParams(q: DetailQuery): Record<string, string | undefined> {
  return {
    sort: q.sort,
    page: q.page > 1 ? String(q.page) : undefined,
    locked: q.locked ? 'true' : undefined,
  };
}

/** Row action callbacks for a recording. */
export interface RecordingHandlers {
  onReview: (_r: WorkspaceRecording) => void;
  onMarkCorrect: (_r: WorkspaceRecording) => void;
  onMarkFalsePositive: (_r: WorkspaceRecording) => void;
  onToggleLock: (_r: WorkspaceRecording) => void;
  onDelete: (_r: WorkspaceRecording) => void;
}
