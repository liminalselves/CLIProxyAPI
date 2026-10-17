import { describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import { readFileSync } from 'node:fs';
import {
  getPagination,
  normalizePaginationParams,
  paginate,
  parsePage,
  parsePageSize,
} from '@/utils/pagination';
import { Pager } from '@/features/sharing/shared';

const invalid = [null, '', '0', '-1', '1.5', 'NaN', 'Infinity', 'abc'];

describe('pagination parameters', () => {
  test('rejects non-positive, fractional, non-numeric and excessive page numbers', () => {
    for (const value of [...invalid, '1000001']) expect(parsePage(value)).toBe(1);
    expect(parsePage('1000000')).toBe(1000000);
    expect(parsePage('2')).toBe(2);
  });

  test('accepts every API-supported size and rejects invalid values', () => {
    for (const value of [...invalid, '201']) expect(parsePageSize(value)).toBe(50);
    for (const size of [1, 20, 50, 100, 200]) expect(parsePageSize(String(size))).toBe(size);
  });

  test('normalizes pagination without removing filters or changing the input', () => {
    const input = new URLSearchParams('scope=site&model=kimi&key_id=key&page=999&page_size=100');
    const next = normalizePaginationParams(input, 151);
    expect(next.get('page')).toBe('2');
    expect(next.get('page_size')).toBe('100');
    for (const key of ['scope', 'model', 'key_id']) expect(next.get(key)).toBe(input.get(key));
    expect(input.get('page')).toBe('999');
    expect(normalizePaginationParams(next, 151).toString()).toBe(next.toString());
  });

  test('omits defaults and only clamps to the result after its total is known', () => {
    const input = new URLSearchParams('page=3&page_size=50&channel_id=channel');
    expect(normalizePaginationParams(input).get('page')).toBe('3');
    expect(normalizePaginationParams(input, 0).toString()).toBe('channel_id=channel');
    expect(normalizePaginationParams(new URLSearchParams('page=NaN&page_size=0')).toString()).toBe(
      ''
    );
  });
});

describe('local pagination boundaries', () => {
  test('keeps exact page boundaries, empty results and the final partial page valid', () => {
    expect(getPagination(100, 2, 50)).toEqual({ currentPage: 2, totalPages: 2, start: 50 });
    expect(getPagination(0, 4, 50)).toEqual({ currentPage: 1, totalPages: 1, start: 0 });
    const items = Array.from({ length: 101 }, (_, i) => i);
    expect(paginate(items, 3, 50).pageItems).toEqual([100]);
    expect(paginate(items.slice(0, 55), 3, 50).pageItems).toEqual([50, 51, 52, 53, 54]);
    expect(items).toHaveLength(101);
  });
});

describe('pager rendering', () => {
  const render = (page: number, total: number, pageSize = 50, loading = false, locale = 'en') => {
    const i18n = createInstance();
    const translation = JSON.parse(readFileSync(`src/i18n/locales/${locale}.json`, 'utf8'));
    void i18n.init({ lng: locale, initAsync: false, resources: { [locale]: { translation } } });
    return renderToStaticMarkup(
      createElement(
        I18nextProvider,
        { i18n },
        createElement(Pager, {
          page,
          total,
          pageSize,
          loading,
          onPage: () => {},
          onPageSize: () => {},
        })
      )
    );
  };

  test('shows both the page count and item count and blocks the correct boundary button', () => {
    const first = render(1, 101);
    expect(first).toContain('Page 1 / 3');
    expect(first.match(/disabled=""/g)).toHaveLength(1);
    const last = render(3, 101);
    expect(last).toContain('Page 3 / 3');
    expect(last.match(/disabled=""/g)).toHaveLength(1);
    expect(render(1, 0).match(/disabled=""/g)).toHaveLength(2);
  });

  test('disables page navigation and size changes while a request is pending', () => {
    const markup = render(2, 151, 50, true);
    expect(markup).toContain('aria-busy="true"');
    expect(markup).toContain('Loading...');
    expect(markup.match(/disabled=""/g)).toHaveLength(3);
    expect(markup).toContain('role="status"');
  });

  test('retains custom API-supported page sizes in the selector', () => {
    expect(render(1, 151, 20)).toContain('>20<');
  });

  test('resolves the new total-pages placeholder in every supported locale', () => {
    for (const locale of ['zh-CN', 'zh-TW', 'en', 'ru']) {
      const markup = render(2, 151, 50, false, locale);
      expect(markup).toContain('2 / 4');
      expect(markup).toContain('151');
      expect(markup).not.toContain('{{');
    }
  });
});
