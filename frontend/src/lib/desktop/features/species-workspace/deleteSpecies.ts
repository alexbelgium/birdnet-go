/**
 * Deletes every unlocked detection of a species in bounded server chunks.
 * Loops until the server reports nothing left, with no pass cap. It stops with
 * an error when a chunk fails some rows (they stay eligible for a retry) or
 * makes no progress (so a row that can never be removed cannot loop forever).
 */
import { deleteSpeciesChunk } from './api';
import type { DeleteChunkResult } from './types';

export interface DeleteProgress {
  deleted: number;
  locked: number;
  reassigned: number;
  remaining: number;
}

export class SpeciesDeleteError extends Error {
  constructor(
    readonly reason: 'failed' | 'stalled',
    readonly progress: DeleteProgress,
    readonly failedIds: string[] = []
  ) {
    super(`Species delete ${reason}`);
    this.name = 'SpeciesDeleteError';
  }
}

export async function deleteAllUnlocked(
  scientificName: string,
  options: {
    signal?: AbortSignal;
    onProgress?: (_progress: DeleteProgress) => void;
    chunk?: (_name: string, _signal?: AbortSignal) => Promise<DeleteChunkResult>;
  } = {}
): Promise<DeleteProgress> {
  const chunk = options.chunk ?? deleteSpeciesChunk;
  const progress: DeleteProgress = { deleted: 0, locked: 0, reassigned: 0, remaining: 0 };
  for (;;) {
    options.signal?.throwIfAborted();
    const result = await chunk(scientificName, options.signal);
    progress.deleted += result.deleted;
    progress.locked += result.locked;
    progress.reassigned += result.reassigned;
    progress.remaining = result.remaining;
    options.onProgress?.({ ...progress });
    if (result.failedIds.length > 0) {
      throw new SpeciesDeleteError('failed', { ...progress }, result.failedIds);
    }
    if (result.remaining <= 0) return progress;
    if (result.deleted + result.reassigned + result.locked === 0) {
      throw new SpeciesDeleteError('stalled', { ...progress });
    }
  }
}
