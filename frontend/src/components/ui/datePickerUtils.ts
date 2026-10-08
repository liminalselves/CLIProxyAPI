/** Pure date helpers for DatePickerInput (UTC-anchored to stay timezone-safe). */

export const DAY_MS = 86400000;

export const utcMidnight = (time: number) => {
  const d = new Date(time);
  return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
};

export const startOfMonth = (time: number) => {
  const d = new Date(time);
  return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1);
};

export const addMonths = (time: number, months: number) => {
  const d = new Date(time);
  return Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + months, 1);
};

export const parseDay = (value: string): number | null => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
  if (!match) return null;
  const time = Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  return Number.isNaN(time) ? null : time;
};

export const formatDay = (time: number) =>
  `${new Date(time).getUTCFullYear()}-${String(new Date(time).getUTCMonth() + 1).padStart(2, '0')}-${String(
    new Date(time).getUTCDate()
  ).padStart(2, '0')}`;

/** 42 UTC day timestamps covering the month grid, aligned to the given first weekday (1=Mon..7=Sun). */
export const buildMonthCells = (viewMonth: number, weekStart: number) => {
  const leading = (new Date(viewMonth).getUTCDay() - (weekStart % 7) + 7) % 7;
  const start = viewMonth - leading * DAY_MS;
  return Array.from({ length: 42 }, (_, i) => start + i * DAY_MS);
};

/** First day of the week (1 = Monday ... 7 = Sunday) for the active locale, Sunday fallback. */
export const localeWeekStart = (locale: string | undefined): number => {
  try {
    const info = (new Intl.Locale(locale || 'en') as Intl.Locale & {
      weekInfo?: { firstDay?: number };
    }).weekInfo;
    const firstDay = info?.firstDay;
    return typeof firstDay === 'number' && firstDay >= 1 && firstDay <= 7 ? firstDay : 7;
  } catch {
    return 7;
  }
};

/** Seven short weekday names starting at the locale's first weekday. */
export const buildWeekdayLabels = (locale: string | undefined, weekStart: number) => {
  const formatter = new Intl.DateTimeFormat(locale, { weekday: 'short', timeZone: 'UTC' });
  return Array.from({ length: 7 }, (_, i) => {
    // 2024-01-07 is a Sunday; align it to the locale's first weekday.
    const day = (((weekStart % 7) + i) % 7);
    return formatter.format(new Date(Date.UTC(2024, 0, 7 + day)));
  });
};
