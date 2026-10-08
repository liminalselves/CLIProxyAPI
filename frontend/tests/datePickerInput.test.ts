import { expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import {
  addMonths,
  buildMonthCells,
  buildWeekdayLabels,
  formatDay,
  localeWeekStart,
  parseDay,
} from '@/components/ui/datePickerUtils';
import { DatePickerInput } from '@/components/ui/DatePickerInput';

test('parseDay and formatDay round-trip ISO day strings', () => {
  expect(formatDay(parseDay('2026-10-06')!)).toBe('2026-10-06');
  expect(formatDay(parseDay('2024-02-29')!)).toBe('2024-02-29');
  expect(parseDay('2026-10-6')).toBeNull();
  expect(parseDay('not-a-date')).toBeNull();
  expect(parseDay('')).toBeNull();
});

test('addMonths crosses year boundaries and normalizes overflow', () => {
  const december = Date.UTC(2026, 11, 1);
  expect(formatDay(addMonths(december, 1))).toBe('2027-01-01');
  expect(formatDay(addMonths(december, -12))).toBe('2025-12-01');
  expect(formatDay(addMonths(december, 14))).toBe('2028-02-01');
});

test('month grid aligns leading days to the first weekday', () => {
  // 2026-10-01 is a Thursday.
  const october = Date.UTC(2026, 9, 1);
  const sundayFirst = buildMonthCells(october, 7);
  expect(sundayFirst).toHaveLength(42);
  expect(formatDay(sundayFirst[0])).toBe('2026-09-27');
  expect(formatDay(sundayFirst[4])).toBe('2026-10-01');
  const mondayFirst = buildMonthCells(october, 1);
  expect(formatDay(mondayFirst[0])).toBe('2026-09-28');
  expect(formatDay(mondayFirst[3])).toBe('2026-10-01');
  // A month starting exactly on the first weekday needs no leading days.
  const february = Date.UTC(2026, 1, 1); // 2026-02-01 is a Sunday
  expect(formatDay(buildMonthCells(february, 7)[0])).toBe('2026-02-01');
});

test('weekday labels start at the given first weekday', () => {
  // weekInfo is unavailable in small-ICU runtimes; the resolver must fall back to Sunday.
  expect([1, 7]).toContain(localeWeekStart('de-DE'));
  expect(localeWeekStart('en-US')).toBe(7);
  expect(localeWeekStart(undefined)).toBe(7);
  const english = buildWeekdayLabels('en-US', 7);
  expect(english[0]).toBe('Sun');
  expect(english[6]).toBe('Sat');
  const german = buildWeekdayLabels('de-DE', 1);
  expect(german[0]).toBe('Mo');
  expect(german[6]).toBe('So');
});

test('trigger renders label and clear affordance without a native date input', () => {
  const empty = renderToStaticMarkup(
    createElement(DatePickerInput, { label: 'From', value: '', onChange: () => {} })
  );
  expect(empty).not.toContain('type="date"');
  expect(empty).toContain('aria-haspopup="dialog"');
  expect(empty).toContain('aria-expanded="false"');
  expect((empty.match(/<button/g) || []).length).toBe(1);

  const withValue = renderToStaticMarkup(
    createElement(DatePickerInput, { label: 'To', value: '2026-10-06', onChange: () => {} })
  );
  expect((withValue.match(/<button/g) || []).length).toBe(2);
});
