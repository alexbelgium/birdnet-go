/** Typed client for the species workspace API. Every call accepts an AbortSignal. */
import { fetchWithCSRF } from '$lib/utils/api';
import type { Detection } from '$lib/types/detection.types';
import type {
  BestRecording,
  DeleteChunkResult,
  MembershipKind,
  Memberships,
  RangeScore,
  WorkspaceLayout,
  WorkspaceSort,
  WorkspaceSpecies,
  WorkspaceSpeciesStats,
} from './types';

const BASE = '/api/v2/species-workspace';

export interface WorkspaceRecording extends Detection {
  modelName: string | null;
}

export interface RecordingsPage {
  data: WorkspaceRecording[];
  total: number;
  page: number;
  perPage: number;
  totalPages: number;
}

export function fetchSpecies(signal?: AbortSignal, scientificName?: string) {
  const query = scientificName ? `?species=${encodeURIComponent(scientificName)}` : '';
  return fetchWithCSRF<WorkspaceSpecies[]>(`${BASE}/species${query}`, { signal });
}

export function fetchStats(signal?: AbortSignal) {
  return fetchWithCSRF<WorkspaceSpeciesStats[]>(`${BASE}/species/stats`, { signal });
}

export function fetchMemberships(signal?: AbortSignal) {
  return fetchWithCSRF<Memberships>(`${BASE}/memberships`, { signal });
}

export function putMembership(kind: MembershipKind, scientificName: string, present: boolean) {
  return fetchWithCSRF<{ kind: MembershipKind; scientificName: string; present: boolean }>(
    `${BASE}/memberships/${kind}`,
    { method: 'PUT', body: { scientificName, present } }
  );
}

export async function fetchRangeScores(signal?: AbortSignal): Promise<Map<string, number>> {
  const data = await fetchWithCSRF<{ species?: RangeScore[] | null }>(
    '/api/v2/range/species/scores?names=false',
    { signal }
  );
  const out = new Map<string, number>();
  for (const entry of data.species ?? []) {
    if (typeof entry.score === 'number') out.set(entry.scientificName, entry.score);
  }
  return out;
}

export function fetchBestRecordings(names: string[], signal?: AbortSignal) {
  const query = new URLSearchParams();
  for (const name of names) query.append('species', name);
  return fetchWithCSRF<Record<string, BestRecording | null>>(`${BASE}/best-recordings?${query}`, {
    signal,
  });
}

export function fetchRecordings(
  params: { species: string; page: number; perPage: number; sort: WorkspaceSort; locked: boolean },
  signal?: AbortSignal
) {
  const query = new URLSearchParams({
    species: params.species,
    page: String(params.page),
    perPage: String(params.perPage),
    sort: params.sort,
  });
  if (params.locked) query.set('locked', 'true');
  return fetchWithCSRF<RecordingsPage>(`${BASE}/recordings?${query}`, { signal });
}

export function deleteSpeciesChunk(scientificName: string, signal?: AbortSignal) {
  return fetchWithCSRF<DeleteChunkResult>(`${BASE}/species/delete`, {
    method: 'POST',
    body: { scientificName },
    signal,
  });
}

export function fetchLayout(signal?: AbortSignal) {
  return fetchWithCSRF<WorkspaceLayout>(`${BASE}/layout`, { signal });
}

export function saveLayout(layout: WorkspaceLayout) {
  return fetchWithCSRF<WorkspaceLayout>(`${BASE}/layout`, { method: 'PUT', body: layout });
}

export interface BatchResult {
  processed: number;
  skipped: number;
}

export function batchReview(ids: string[], verified: 'correct' | 'false_positive') {
  return fetchWithCSRF<BatchResult>('/api/v2/detections/batch/review', {
    method: 'POST',
    body: { ids, verified },
  });
}

export function batchLock(ids: string[], locked: boolean) {
  return fetchWithCSRF<BatchResult>('/api/v2/detections/batch/lock', {
    method: 'POST',
    body: { ids, locked },
  });
}

export function batchDelete(ids: string[]) {
  return fetchWithCSRF<BatchResult>('/api/v2/detections/batch/delete', {
    method: 'POST',
    body: { ids },
  });
}
