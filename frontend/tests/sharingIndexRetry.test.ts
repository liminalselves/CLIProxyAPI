import { describe, expect, spyOn, test } from 'bun:test';
import {
  JOURNAL_INDEX_BUILDING,
  retryWhileIndexBuilding,
  sharingApi,
} from '../src/services/api/sharing';
import { apiClient } from '../src/services/api/client';

type ErrorWithCode = Error & { status?: number; apiCode?: string };

const indexBuildingError = (): ErrorWithCode => {
  const error: ErrorWithCode = new Error(
    'usage history index is being prepared; retry shortly'
  );
  error.status = 503;
  error.apiCode = JOURNAL_INDEX_BUILDING;
  return error;
};

describe('journal index building retry', () => {
  test('retries only the explicit 503 index-building state until it resolves', async () => {
    let attempts = 0;
    const result = await retryWhileIndexBuilding(
      async () => {
        attempts += 1;
        if (attempts < 3) throw indexBuildingError();
        return 'ok';
      },
      undefined,
      5
    );
    expect(result).toBe('ok');
    expect(attempts).toBe(3);
  });

  test('other 503 codes and non-503 statuses reject immediately', async () => {
    const others: ErrorWithCode[] = [
      Object.assign(new Error('other 503'), { status: 503, apiCode: 'unavailable' }),
      Object.assign(new Error('server error'), { status: 500, apiCode: JOURNAL_INDEX_BUILDING }),
    ];
    for (const error of others) {
      let attempts = 0;
      await expect(
        retryWhileIndexBuilding(
          async () => {
            attempts += 1;
            throw error;
          },
          undefined,
          5
        )
      ).rejects.toBe(error);
      expect(attempts).toBe(1);
    }
  });

  test('aborting the signal cancels the pending wait and stops retrying', async () => {
    const controller = new AbortController();
    let attempts = 0;
    const pending = retryWhileIndexBuilding(
      async () => {
        attempts += 1;
        throw indexBuildingError();
      },
      controller.signal,
      1000
    );
    // Let the first attempt reject and the retry wait start, then abort.
    await new Promise((resolve) => setTimeout(resolve, 10));
    controller.abort();
    await expect(pending).rejects.toThrow();
    expect(attempts).toBe(1);
  });

  test('every attempt receives the caller signal so in-flight GETs cancel too', async () => {
    const controller = new AbortController();
    const seen: (AbortSignal | undefined)[] = [];
    let attempts = 0;
    const result = await retryWhileIndexBuilding(
      async (signal) => {
        seen.push(signal);
        attempts += 1;
        if (attempts === 1) throw indexBuildingError();
        return 'ok';
      },
      controller.signal,
      5
    );
    expect(result).toBe('ok');
    expect(seen[0]).toBe(controller.signal);
    expect(seen[1]).toBe(controller.signal);
  });

  test('summary requests retry through the wrapper and then resolve', async () => {
    const get = spyOn(apiClient, 'get')
      .mockRejectedValueOnce(indexBuildingError())
      .mockResolvedValue({ total: { calls: 0 }, groups: [] });
    try {
      const result = await sharingApi.summary({ consumer: 'alice' });
      expect(result.groups).toEqual([]);
      expect(get).toHaveBeenCalledTimes(2);
      expect(get).toHaveBeenCalledWith('/v8/hub/usage/summary', {
        params: { consumer: 'alice' },
        signal: undefined,
      });
    } finally {
      get.mockRestore();
    }
  });
});
