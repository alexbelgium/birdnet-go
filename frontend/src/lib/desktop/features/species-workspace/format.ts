/** Locale-aware formatting for the workspace. */
import { getLocale } from '$lib/i18n';

export function formatCount(value: number): string {
  return new Intl.NumberFormat(getLocale()).format(value);
}

/** Formats a 0..1 fraction as a percentage. */
export function formatPercent(fraction: number, digits = 0): string {
  return new Intl.NumberFormat(getLocale(), {
    style: 'percent',
    maximumFractionDigits: digits,
  }).format(fraction);
}

/** Formats an RFC3339 timestamp as a short date and time, or '' when missing. */
export function formatDateTime(iso: string | undefined): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'short' }).format(
    date
  );
}
