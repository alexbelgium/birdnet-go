/**
 * A request slot runs at most one request at a time: starting a new request
 * aborts the one in flight and replaces it, so a newer request is never dropped
 * and a stale response is never committed.
 */
export interface RequestSlot {
  run<T>(work: (signal: AbortSignal) => Promise<T>): Promise<T>;
  abort(): void;
}

export class SupersededError extends Error {
  constructor() {
    super('Request superseded');
    this.name = 'AbortError';
  }
}

export function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError';
}

export function createRequestSlot(): RequestSlot {
  let controller: AbortController | null = null;
  return {
    async run<T>(work: (signal: AbortSignal) => Promise<T>): Promise<T> {
      controller?.abort();
      const mine = new AbortController();
      controller = mine;
      try {
        const result = await work(mine.signal);
        if (mine.signal.aborted) throw new SupersededError();
        return result;
      } catch (error) {
        if (mine.signal.aborted) throw new SupersededError();
        throw error;
      } finally {
        if (controller === mine) controller = null;
      }
    },
    abort() {
      controller?.abort();
      controller = null;
    },
  };
}
