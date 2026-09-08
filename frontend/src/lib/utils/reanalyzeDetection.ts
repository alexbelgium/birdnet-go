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

const inFlightReanalyze = new Set<number>();
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
 * Duplicate requests for the same detection are dropped while one is in flight,
 * so a double-click cannot spend inference twice. Returns `null` in that case.
 */
export async function reanalyzeDetection(
  detectionId: number,
  modelIds?: string[]
): Promise<ReanalyzeResult | null> {
  if (inFlightReanalyze.has(detectionId)) return null;

  inFlightReanalyze.add(detectionId);
  try {
    return await fetchWithCSRF<ReanalyzeResult>(`/api/v2/detections/${detectionId}/reanalyze`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ modelIds: modelIds ?? [] }),
      timeout: REANALYZE_TIMEOUT_MS,
    });
  } catch (err) {
    logger.error('Reanalyze request failed:', err, {
      component: 'reanalyzeDetection',
      detectionId,
      modelIds,
    });
    throw err;
  } finally {
    inFlightReanalyze.delete(detectionId);
  }
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
