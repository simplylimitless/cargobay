// Timezone-aware date/time formatting. All timestamps are stored in UTC
// (Postgres TIMESTAMPTZ) — the `timezone` preference passed here only
// controls how they're rendered. An empty string falls back to the
// browser's local timezone (today's behavior for users with no preference).

export const COMMON_TIMEZONES = [
  'UTC',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'America/Anchorage',
  'America/Sao_Paulo',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Moscow',
  'Africa/Cairo',
  'Africa/Johannesburg',
  'Asia/Dubai',
  'Asia/Kolkata',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Seoul',
  'Asia/Singapore',
  'Australia/Sydney',
  'Pacific/Auckland',
]

type IntlWithSupportedValuesOf = typeof Intl & { supportedValuesOf?: (key: string) => string[] }

export function listTimezones(): string[] {
  const intl = Intl as IntlWithSupportedValuesOf
  if (typeof intl.supportedValuesOf === 'function') {
    try {
      return intl.supportedValuesOf('timeZone')
    } catch {
      // fall through to the static list
    }
  }
  return COMMON_TIMEZONES
}

function toDate(value: string | Date): Date {
  return value instanceof Date ? value : new Date(value)
}

export function formatDateTime(value: string | Date, timezone: string, opts?: Intl.DateTimeFormatOptions): string {
  return toDate(value).toLocaleString(undefined, { timeZone: timezone || undefined, ...opts })
}

export function formatDate(value: string | Date, timezone: string): string {
  return toDate(value).toLocaleDateString(undefined, { timeZone: timezone || undefined })
}

export function formatTime(value: string | Date, timezone: string): string {
  return toDate(value).toLocaleTimeString(undefined, { timeZone: timezone || undefined })
}
