import type { MarketplaceModel, ModelPerformance, PerformanceBucket } from './types';

export interface MarketplaceModelGroup {
  baseModel: string;
  routes: MarketplaceModel[];
}

interface MarketplaceFilter {
  search?: string;
  owner?: string;
  provider?: string;
}

export function groupMarketplaceModels(
  models: MarketplaceModel[],
  { search = '', owner = '', provider = '' }: MarketplaceFilter = {}
): MarketplaceModelGroup[] {
  const query = search.trim().toLowerCase();
  const groups = new Map<string, MarketplaceModelGroup>();

  for (const model of models) {
    const baseModel = model.base_model.trim();
    if (
      !baseModel ||
      (owner && model.owner !== owner) ||
      (provider && model.provider !== provider)
    ) {
      continue;
    }
    const searchable = [baseModel, model.id, model.display_name, model.channel_slug, model.owner];
    if (query && !searchable.some((value) => value.toLowerCase().includes(query))) continue;

    let group = groups.get(baseModel);
    if (!group) {
      group = { baseModel, routes: [] };
      groups.set(baseModel, group);
    }
    group.routes.push(model);
  }

  for (const group of groups.values()) {
    group.routes.sort(
      (left, right) =>
        Number(right.available) - Number(left.available) ||
        left.id.localeCompare(right.id, undefined, { numeric: true })
    );
  }
  return [...groups.values()].sort((left, right) =>
    left.baseModel.localeCompare(right.baseModel, undefined, { numeric: true })
  );
}

export function marketplaceUsagePath(route: MarketplaceModel): string {
  const query = new URLSearchParams({ channel_id: route.channel_id, model: route.id });
  return `/usage?${query}`;
}

export interface PerformanceMetrics {
  requests: number;
  /** null when there is nothing to measure (no requests / no samples). */
  successRate: number | null;
  avgLatencyMS: number | null;
  avgTTFTMS: number | null;
  /** Output tokens per second of generation time. */
  tps: number | null;
}

export function performanceMetrics(bucket: PerformanceBucket): PerformanceMetrics {
  return {
    requests: bucket.requests,
    successRate: bucket.requests > 0 ? (bucket.requests - bucket.failures) / bucket.requests : null,
    avgLatencyMS: bucket.latency_count > 0 ? bucket.latency_ms / bucket.latency_count : null,
    avgTTFTMS: bucket.ttft_count > 0 ? bucket.ttft_ms / bucket.ttft_count : null,
    tps:
      bucket.generation_ms > 0 && bucket.output_tokens > 0
        ? (bucket.output_tokens * 1000) / bucket.generation_ms
        : null,
  };
}

const emptyBucket = (): PerformanceBucket => ({
  start: 0,
  requests: 0,
  failures: 0,
  latency_ms: 0,
  latency_count: 0,
  ttft_ms: 0,
  ttft_count: 0,
  output_tokens: 0,
  generation_ms: 0,
});

const addBucket = (into: PerformanceBucket, from: PerformanceBucket) => {
  into.requests += from.requests;
  into.failures += from.failures;
  into.latency_ms += from.latency_ms;
  into.latency_count += from.latency_count;
  into.ttft_ms += from.ttft_ms;
  into.ttft_count += from.ttft_count;
  into.output_tokens += from.output_tokens;
  into.generation_ms += from.generation_ms;
};

/** Merge several routes of one base model into overall totals and hourly buckets. */
export function mergePerformance(entries: ModelPerformance[]): ModelPerformance | null {
  if (!entries.length) return null;
  const totals = emptyBucket();
  const buckets: PerformanceBucket[] = entries[0].buckets.map((bucket) => ({
    ...emptyBucket(),
    start: bucket.start,
  }));
  for (const entry of entries) {
    addBucket(totals, entry.totals);
    entry.buckets.forEach((bucket, index) => {
      if (buckets[index]) addBucket(buckets[index], bucket);
    });
  }
  return { model: '', totals, buckets };
}

/** Card-level highlight: best (lowest) latency and best (highest) TPS across routes. */
export function bestPerformance(entries: ModelPerformance[]): {
  avgLatencyMS: number | null;
  tps: number | null;
} {
  const latencies: number[] = [];
  const rates: number[] = [];
  for (const entry of entries) {
    const metrics = performanceMetrics(entry.totals);
    if (metrics.avgLatencyMS !== null) latencies.push(metrics.avgLatencyMS);
    if (metrics.tps !== null) rates.push(metrics.tps);
  }
  return {
    avgLatencyMS: latencies.length ? Math.min(...latencies) : null,
    tps: rates.length ? Math.max(...rates) : null,
  };
}

export const formatMS = (value: number | null): string =>
  value === null ? '—' : value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${Math.round(value)}ms`;

export const formatTPS = (value: number | null): string =>
  value === null ? '—' : value.toFixed(1);

export const formatPercent = (value: number | null): string =>
  value === null ? '—' : `${(value * 100).toFixed(2)}%`;

/** Compact context sizes the way new-api prints them: 131K, 1M. */
export const compactTokens = (value: number): string =>
  value >= 1_000_000
    ? `${+(value / 1_000_000).toFixed(2)}M`
    : value >= 1000
      ? `${Math.round(value / 1000)}K`
      : String(value);
