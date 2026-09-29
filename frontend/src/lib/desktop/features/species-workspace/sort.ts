/** Registry-driven comparator: missing values sort last in both directions, ties by scientific name. */

export type SortValue = number | string | null | undefined;

export function compareValues(
  a: SortValue,
  b: SortValue,
  direction: 'asc' | 'desc',
  collator: Intl.Collator
): number {
  const aMissing = a === null || a === undefined || (typeof a === 'number' && Number.isNaN(a));
  const bMissing = b === null || b === undefined || (typeof b === 'number' && Number.isNaN(b));
  if (aMissing || bMissing) return aMissing === bMissing ? 0 : aMissing ? 1 : -1;
  const cmp =
    typeof a === 'number' && typeof b === 'number' ? a - b : collator.compare(String(a), String(b));
  return direction === 'asc' ? cmp : -cmp;
}

export function sortRows<T extends { scientificName: string }>(
  rows: readonly T[],
  value: (row: T) => SortValue,
  direction: 'asc' | 'desc',
  locale: string
): T[] {
  const collator = new Intl.Collator(locale, { sensitivity: 'base', numeric: true });
  return [...rows].sort(
    (x, y) =>
      compareValues(value(x), value(y), direction, collator) ||
      collator.compare(x.scientificName, y.scientificName)
  );
}
