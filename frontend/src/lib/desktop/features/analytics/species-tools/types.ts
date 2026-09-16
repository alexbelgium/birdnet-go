export interface SpeciesRow {
  scientific_name: string;
  common_name: string;
  species_code?: string;
  count?: number;
  max_confidence?: number;
  last_heard?: string;
}
export interface Recording {
  id: number;
  scientific_name: string;
  confidence: number;
  locked: boolean;
}
export const columns = [
  'count',
  'max_confidence',
  'last_heard',
  'correct',
  'range',
  'best',
  'confirmed',
  'included',
  'excluded',
] as const;
export type Column = (typeof columns)[number];
export type SortKey = Column | 'name';
export type Membership = 'confirmed' | 'included' | 'excluded';
export const membershipNames: Membership[] = ['confirmed', 'included', 'excluded'];
export const defaultColumns: Column[] = ['count', 'max_confidence', 'last_heard', 'best'];
export const metricColumns = ['count', 'max_confidence', 'last_heard'] as const;
export function isColumn(value: unknown): value is Column {
  return typeof value === 'string' && columns.some(column => column === value);
}
