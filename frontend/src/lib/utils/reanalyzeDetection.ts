/**
 * API client for the reanalysis + correction endpoints.
 *
 * Both endpoints are fork-local additions served by internal/api/v2/reanalyze.
 * Kept in its own module (rather than folded into an existing api helper) so the
 * feature stays confined to files upstream does not own.
 */
import { fetchWithCSRF } from './api';
import { loggers } from './logger';

const logger = loggers.ui;

/** A model that participated in a reanalysis run, returned in modelsRun. */
export interface ReanalyzeModelInfo {
  id: string;
  name: string;
  sampleRate: number;
  windowCount: number;
}

/**
 * One species row of a multi-model reanalysis. `byModel` maps each participating
 * model's registry ID to the highest confidence that model produced; an absent
 * key means the model did not predict the species in any window at all.
 */
export interface ReanalyzePrediction {
  scientificName: string;
  commonName?: string;
  byModel: Record<string, number>;
}

/** Response shape of POST /api/v2/detections/:id/reanalyze. */
export interface ReanalyzeResult {
  detectionId: number;
  clipDurationSec: number;
  modelsRun: ReanalyzeModelInfo[];
  predictions: ReanalyzePrediction[];
}

/** Body of POST /api/v2/detections/:id/correct-species. */
export interface CorrectSpeciesRequest {
  scientificName: string;
  modelId: string;
  confidence: number;
}

/** Response shape of the correction endpoint. */
export interface CorrectSpeciesResult {
  detectionId: number;
  scientificName: string;
  commonName: string;
  modelId: string;
  modelName: string;
  confidence: number;
  verified: string;
}

/**
 * In-flight reanalyses, keyed by detection id, holding the PROMISE rather than
 * just a marker. A second caller for the same detection joins the running
 * request instead of being told "no".
 *
 * That distinction is the whole point: a marker-and-null design loses the result
 * when the user closes and reopens the modal while a request is still running —
 * the reopened modal gets null, stops its spinner, and sits empty forever
 * because the original response is discarded as superseded. Sharing the promise
 * is safe here because reanalysis is a read-only, argument-independent query for
 * a given detection.
 */
const inFlightReanalyze = new Map<number, Promise<ReanalyzeResult>>();

/**
 * In-flight corrections, as a plain marker set. Corrections are NOT shareable
 * the way reanalyses are: two requests for the same detection may assert
 * different species, so handing the second caller the first one's result would
 * report a correction that never happened. Duplicates are dropped instead.
 */
const inFlightCorrection = new Set<number>();

/**
 * fetchWithCSRF's 30 s default is too aggressive for reanalysis. The classifier
 * orchestrator serialises inference across every model process-wide, so a long
 * clip queues behind whatever the realtime pipeline is doing. The backend caps
 * decode at 60 s and inference adds seconds on top, so 2 minutes is comfortable
 * headroom and anything past it is a genuine hang worth surfacing.
 */
const REANALYZE_TIMEOUT_MS = 120_000;

/**
 * Re-run inference on a saved detection's audio clip. Runs every loaded
 * compatible classifier by default; pass `modelIds` to restrict. The server does
 * not persist anything returned here — callers display it transiently.
 *
 * A second call for a detection that is already being reanalyzed joins the
 * running request and resolves with the same result, so a double-click (or a
 * close-and-reopen of the modal) never spends inference twice and never loses
 * the answer. The server enforces its own single-slot admission control on top;
 * this only keeps one client from racing itself.
 */
export function reanalyzeDetection(
  detectionId: number,
  modelIds?: string[]
): Promise<ReanalyzeResult> {
  const existing = inFlightReanalyze.get(detectionId);
  if (existing) return existing;

  const request = fetchWithCSRF<ReanalyzeResult>(`/api/v2/detections/${detectionId}/reanalyze`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ modelIds: modelIds ?? [] }),
    timeout: REANALYZE_TIMEOUT_MS,
  })
    .catch((err: unknown) => {
      logger.error('Reanalyze request failed:', err, {
        component: 'reanalyzeDetection',
        detectionId,
        modelIds,
      });
      throw err;
    })
    .finally(() => {
      inFlightReanalyze.delete(detectionId);
    });

  inFlightReanalyze.set(detectionId, request);
  return request;
}

/**
 * Apply a species correction to a saved detection: replaces its species,
 * attributed model and confidence with the operator's chosen prediction and marks
 * it verified='correct', atomically on the server.
 *
 * Duplicate requests for the same detection are dropped while one is in flight.
 * Returns `null` in that case.
 */
export async function correctDetectionSpecies(
  detectionId: number,
  payload: CorrectSpeciesRequest
): Promise<CorrectSpeciesResult | null> {
  if (inFlightCorrection.has(detectionId)) return null;

  inFlightCorrection.add(detectionId);
  try {
    return await fetchWithCSRF<CorrectSpeciesResult>(
      `/api/v2/detections/${detectionId}/correct-species`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }
    );
  } catch (err) {
    logger.error('Correction request failed:', err, {
      component: 'correctDetectionSpecies',
      detectionId,
      payload,
    });
    throw err;
  } finally {
    inFlightCorrection.delete(detectionId);
  }
}
