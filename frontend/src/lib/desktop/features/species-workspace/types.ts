/** Wire types for /api/v2/species-workspace/*. */

export interface WorkspaceSpecies {
  scientificName: string;
  commonName: string;
  speciesCode: string;
  /** Every stored detection, false positives included (what a delete acts on). */
  total: number;
  /** Locked detections (kept by a delete). */
  locked: number;
  firstSeen: string;
  lastSeen: string;
}

export interface WorkspaceSpeciesStats {
  scientificName: string;
  correct: number;
  falsePositive: number;
  /** Highest confidence among non-false-positive detections with a recording. */
  maxConfidence: number | null;
}

export type MembershipKind = 'confirmed' | 'included' | 'excluded';

export type Memberships = Record<MembershipKind, string[]>;

export interface BestRecording {
  id: number;
  confidence: number;
  locked: boolean;
}

export type WorkspaceSort = 'confidence_desc' | 'confidence_asc' | 'date_desc' | 'date_asc';

export interface DeleteChunkResult {
  deleted: number;
  /** Kept because they were locked while the chunk ran. */
  locked: number;
  /** Kept because they moved to another species while the chunk ran. */
  reassigned: number;
  /** Unlocked detections of the species still stored. */
  remaining: number;
}

export interface LayoutColumn {
  id: string;
  visible: boolean;
}

export interface WorkspaceLayout {
  columns: LayoutColumn[];
  sort: { column: string; direction: 'asc' | 'desc' };
}

export interface RangeScore {
  scientificName: string;
  score?: number;
}
