/** Row model shared by the table and card views. */
import type { ColumnId } from '../columns';
import type { SortValue } from '../sort';
import type { WorkspaceData } from '../workspaceData.svelte';
import type { WorkspaceSpecies } from '../types';

export interface SpeciesRow extends WorkspaceSpecies {
  displayName: string;
}

/** Sort value of a row for a column; null while its data is not loaded. */
export function columnSortValue(column: ColumnId, row: SpeciesRow, data: WorkspaceData): SortValue {
  switch (column) {
    case 'species':
      return row.displayName;
    case 'count':
      return row.total;
    case 'maxConfidence':
      return data.stats.get(row.scientificName)?.maxConfidence ?? null;
    case 'firstSeen':
      return row.firstSeen ? Date.parse(row.firstSeen) : null;
    case 'lastSeen':
      return row.lastSeen ? Date.parse(row.lastSeen) : null;
    case 'range':
      return data.ranges.get(row.scientificName) ?? null;
    case 'verification':
      return verificationRatio(data, row.scientificName);
    case 'excluded':
    case 'included':
    case 'confirmed': {
      const member = data.isMember(column, row.scientificName);
      return member === null ? null : Number(member);
    }
    case 'bestRecording':
    case 'actions':
      return null;
  }
}

/** Correct share of reviewed detections, or null when none are reviewed. */
export function verificationRatio(data: WorkspaceData, scientificName: string): number | null {
  const stat = data.stats.get(scientificName);
  if (!stat) return null;
  const reviewed = stat.correct + stat.falsePositive;
  return reviewed > 0 ? stat.correct / reviewed : null;
}
