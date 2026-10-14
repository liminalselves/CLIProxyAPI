import { describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import english from '../src/i18n/locales/en.json';
import { MarketplaceModelCard } from '../src/features/sharing/MarketplaceModelCard';
import {
  bestPerformance,
  compactTokens,
  groupMarketplaceModels,
  marketplaceUsagePath,
  mergePerformance,
  performanceMetrics,
} from '../src/features/sharing/marketplace';
import type {
  MarketplaceModel,
  ModelPerformance,
  PerformanceBucket,
} from '../src/features/sharing/types';

const route = (overrides: Partial<MarketplaceModel> = {}): MarketplaceModel => ({
  id: 'alice-kimi/kimi-k2',
  base_model: 'kimi-k2',
  display_name: 'Kimi K2',
  owner: 'alice',
  channel_id: 'alice-channel',
  channel: 'alice-kimi',
  channel_slug: 'alice-pool',
  provider: 'kimi',
  available: true,
  credential_count: 1,
  context_length: 131072,
  ...overrides,
});
const bob = route({
  id: 'bob-kimi/kimi-k2',
  owner: 'bob',
  channel_id: 'bob-channel',
  channel: 'bob-kimi',
  channel_slug: 'bob-pool',
  context_length: 262144,
});
const i18n = createInstance();
void i18n.init({ lng: 'en', initAsync: false, resources: { en: { translation: english } } });

const render = (models: MarketplaceModel[], performance: ModelPerformance[] = []) =>
  renderToStaticMarkup(
    createElement(
      I18nextProvider,
      { i18n },
      createElement(
        MemoryRouter,
        null,
        createElement(MarketplaceModelCard, {
          group: groupMarketplaceModels(models)[0],
          performance,
          onCopy: () => {},
          onShowDetail: () => {},
        })
      )
    )
  );

const bucket = (overrides: Partial<PerformanceBucket> = {}): PerformanceBucket => ({
  start: 1000,
  requests: 0,
  failures: 0,
  latency_ms: 0,
  latency_count: 0,
  ttft_ms: 0,
  ttft_count: 0,
  output_tokens: 0,
  generation_ms: 0,
  ...overrides,
});

const perf = (model: string, totals: Partial<PerformanceBucket>): ModelPerformance => ({
  model,
  totals: bucket(totals),
  buckets: [bucket({ start: 10 }), bucket({ start: 20 })],
});

describe('grouped marketplace listing', () => {
  test('groups identical base IDs across channels into one card', () => {
    const groups = groupMarketplaceModels([bob, route()]);
    expect(groups).toHaveLength(1);
    expect(groups[0].baseModel).toBe('kimi-k2');
    expect(groups[0].routes.map((model) => model.id)).toEqual([
      'alice-kimi/kimi-k2',
      'bob-kimi/kimi-k2',
    ]);
  });

  test('keeps model variants in separate groups and sorts numerically', () => {
    const models = [
      route({ base_model: 'model-10', id: 'a/model-10' }),
      route({ base_model: 'model-2', id: 'a/model-2' }),
      route({ base_model: 'kimi-k2-thinking', id: 'alice-kimi/kimi-k2-thinking' }),
      route(),
    ];
    expect(groupMarketplaceModels(models).map((group) => group.baseModel)).toEqual([
      'kimi-k2',
      'kimi-k2-thinking',
      'model-2',
      'model-10',
    ]);
  });

  test('filters by owner, provider and free-text search before grouping', () => {
    const models = [route(), bob];
    expect(groupMarketplaceModels(models, { owner: 'bob' })[0].routes).toEqual([bob]);
    expect(groupMarketplaceModels(models, { search: '  BOB  ' })[0].routes).toEqual([bob]);
    expect(groupMarketplaceModels(models, { search: 'alice-pool' })[0].routes).toEqual([
      models[0],
    ]);
    expect(groupMarketplaceModels(models, { search: 'kimi-k2' })[0].routes).toHaveLength(2);
    expect(groupMarketplaceModels(models, { provider: 'codex' })).toEqual([]);
    expect(groupMarketplaceModels(models, { search: 'no-match' })).toEqual([]);
  });

  test('sorts unavailable routes after available ones inside a group', () => {
    const unavailable = route({ id: 'aaa/down', available: false });
    const [group] = groupMarketplaceModels([unavailable, bob]);
    expect(group.routes.map((model) => model.id)).toEqual(['bob-kimi/kimi-k2', 'aaa/down']);
  });

  test('encodes model and channel in usage links', () => {
    const selected = route({
      id: 'alice&team/custom/model?thinking=high',
      channel_id: 'channel&one',
    });
    const url = new URL(marketplaceUsagePath(selected), 'http://localhost');
    expect(url.pathname).toBe('/usage');
    expect(url.searchParams.get('channel_id')).toBe(selected.channel_id);
    expect(url.searchParams.get('model')).toBe(selected.id);
  });

  test('card title copies the base model; every channel gets its own copy line', () => {
    const markup = render([route(), bob]);
    const title = markup.match(
      /<h2[^>]*>(<button[^>]*title="Copy model name"[^>]*>[\s\S]*?<\/button>)<\/h2>/g
    );
    expect(title).toHaveLength(1);
    expect(title![0]).toContain('kimi-k2');
    expect(markup).toContain('<code>alice-kimi/kimi-k2</code>');
    expect(markup).toContain('<code>bob-kimi/kimi-k2</code>');
    expect(markup).toContain('Details');
    expect(markup).not.toMatch(/<a[^>]*><button/);
  });

  test('cards stay free of descriptions and dot-chains', () => {
    const markup = render([route({ description: 'Flagship coding model' })]);
    expect(markup).not.toContain('Flagship coding model');
    expect(markup).not.toContain('·');
  });

  test('shows compact context size like new-api', () => {
    expect(compactTokens(131072)).toBe('131K');
    expect(compactTokens(1048576)).toBe('1.05M');
    expect(render([route()])).toContain('131K');
    expect(render([route(), bob])).toContain('131K–262K');
  });

  test('shows dash metrics when there is no performance data', () => {
    const markup = render([route()]);
    expect(markup).toContain('<dt>Latency</dt>');
    expect(markup).toContain('<dt>TPS</dt>');
    expect(markup).toContain('—');
  });

  test('card highlights the best route metrics', () => {
    const markup = render(
      [route(), bob],
      [
        perf('alice-kimi/kimi-k2', { latency_ms: 900, latency_count: 3, output_tokens: 600, generation_ms: 20000 }),
        perf('bob-kimi/kimi-k2', { latency_ms: 200, latency_count: 2, output_tokens: 900, generation_ms: 10000 }),
      ]
    );
    expect(markup).toContain('100ms');
    expect(markup).toContain('90.0');
  });
});

describe('marketplace performance metrics', () => {
  test('derives rates and averages only from available samples', () => {
    const empty = performanceMetrics(bucket());
    expect(empty).toEqual({
      requests: 0,
      successRate: null,
      avgLatencyMS: null,
      avgTTFTMS: null,
      tps: null,
    });
    const metrics = performanceMetrics(
      bucket({
        requests: 4,
        failures: 1,
        latency_ms: 900,
        latency_count: 3,
        ttft_ms: 300,
        ttft_count: 2,
        output_tokens: 600,
        generation_ms: 20000,
      })
    );
    expect(metrics.successRate).toBe(0.75);
    expect(metrics.avgLatencyMS).toBe(300);
    expect(metrics.avgTTFTMS).toBe(150);
    expect(metrics.tps).toBe(30);
  });

  test('bestPerformance picks lowest latency and highest TPS', () => {
    const best = bestPerformance([
      perf('a', { latency_ms: 900, latency_count: 3, output_tokens: 600, generation_ms: 20000 }),
      perf('b', { latency_ms: 200, latency_count: 2, output_tokens: 900, generation_ms: 10000 }),
      perf('empty', {}),
    ]);
    expect(best.avgLatencyMS).toBe(100);
    expect(best.tps).toBe(90);
    expect(bestPerformance([])).toEqual({ avgLatencyMS: null, tps: null });
  });

  test('merges routes into overall totals and hourly buckets without losing starts', () => {
    const entries: ModelPerformance[] = [
      {
        model: 'a/m',
        totals: bucket({ requests: 2, output_tokens: 100, generation_ms: 5000 }),
        buckets: [bucket({ start: 10, requests: 2 }), bucket({ start: 20 })],
      },
      {
        model: 'b/m',
        totals: bucket({ requests: 1, failures: 1, output_tokens: 300, generation_ms: 5000 }),
        buckets: [bucket({ start: 10 }), bucket({ start: 20, requests: 1, failures: 1 })],
      },
    ];
    const merged = mergePerformance(entries);
    expect(merged).not.toBeNull();
    expect(merged!.totals.requests).toBe(3);
    expect(merged!.totals.failures).toBe(1);
    expect(performanceMetrics(merged!.totals).tps).toBe(40);
    expect(merged!.buckets.map((b) => b.start)).toEqual([10, 20]);
    expect(performanceMetrics(merged!.buckets[1]).successRate).toBe(0);
    expect(mergePerformance([])).toBeNull();
  });
});
