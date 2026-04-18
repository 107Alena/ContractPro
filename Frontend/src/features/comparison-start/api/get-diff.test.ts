// Unit-тесты api/get-diff.ts: контракт вызова axios (endpoint с тремя
// path-параметрами, signal), нормализация ответа (дефолты для optional полей),
// pass-through 404 DIFF_NOT_FOUND.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { getDiff, getDiffEndpoint } from './get-diff';
import { __setHttpForTests } from './http';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const BASE_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const TARGET_ID = 'ta39e700-aaaa-bbbb-cccc-222222222222';

const OK_RESPONSE = {
  base_version_id: BASE_ID,
  target_version_id: TARGET_ID,
  text_diff_count: 3,
  structural_diff_count: 2,
  text_diffs: [
    { type: 'added' as const, path: 'p.1.1', old_text: null, new_text: 'Новый текст' },
    { type: 'modified' as const, path: 'p.1.2', old_text: 'A', new_text: 'B' },
    { type: 'removed' as const, path: 'p.1.3', old_text: 'C', new_text: null },
  ],
  structural_diffs: [
    { type: 'added' as const, node_id: 'n1', old_value: null, new_value: { k: 1 } },
    { type: 'moved' as const, node_id: 'n2', old_value: { k: 2 }, new_value: { k: 2 } },
  ],
};

type MockGet = ReturnType<typeof vi.fn>;

function mockHttp(get: MockGet): AxiosInstance {
  return { get } as unknown as AxiosInstance;
}

let getSpy: MockGet;

beforeEach(() => {
  getSpy = vi.fn();
  __setHttpForTests(mockHttp(getSpy));
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('getDiff — call shape', () => {
  it('GET на /contracts/{id}/versions/{base}/diff/{target}', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await getDiff({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });

    expect(getSpy).toHaveBeenCalledTimes(1);
    const [path] = getSpy.mock.calls[0]!;
    expect(path).toBe(getDiffEndpoint(CONTRACT_ID, BASE_ID, TARGET_ID));
    expect(path).toBe(
      `/contracts/${CONTRACT_ID}/versions/${BASE_ID}/diff/${TARGET_ID}`,
    );
  });

  it('все три path-параметра экранируются', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await getDiff({
      contractId: 'a/b',
      baseVersionId: 'v#1',
      targetVersionId: 'v 2',
    });
    const [path] = getSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb/versions/v%231/diff/v%202');
  });

  it('signal передаётся в config, когда задан', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    await getDiff(
      { contractId: CONTRACT_ID, baseVersionId: BASE_ID, targetVersionId: TARGET_ID },
      { signal: controller.signal },
    );
    const [, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });

  it('без signal — ключ signal не попадает в config', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await getDiff({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    const [, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(config).toBeDefined();
    expect('signal' in (config ?? {})).toBe(false);
  });
});

describe('getDiff — response narrow', () => {
  it('camelCase narrowed-response c полным набором полей', async () => {
    getSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await getDiff({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    expect(result.baseVersionId).toBe(BASE_ID);
    expect(result.targetVersionId).toBe(TARGET_ID);
    expect(result.textDiffCount).toBe(3);
    expect(result.structuralDiffCount).toBe(2);
    expect(result.textDiffs).toHaveLength(3);
    expect(result.structuralDiffs).toHaveLength(2);
  });

  it('отсутствующие массивы → пустой массив (детерминированный API)', async () => {
    getSpy.mockResolvedValueOnce({
      data: {
        base_version_id: BASE_ID,
        target_version_id: TARGET_ID,
        text_diff_count: 0,
        structural_diff_count: 0,
      },
    });
    const result = await getDiff({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    expect(result.textDiffs).toEqual([]);
    expect(result.structuralDiffs).toEqual([]);
  });

  it('отсутствующие счётчики → 0', async () => {
    getSpy.mockResolvedValueOnce({
      data: { base_version_id: BASE_ID, target_version_id: TARGET_ID },
    });
    const result = await getDiff({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    expect(result.textDiffCount).toBe(0);
    expect(result.structuralDiffCount).toBe(0);
  });

  it('отсутствующие version_id → подставляются из input (fallback)', async () => {
    getSpy.mockResolvedValueOnce({ data: { text_diff_count: 0, structural_diff_count: 0 } });
    const result = await getDiff({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    expect(result.baseVersionId).toBe(BASE_ID);
    expect(result.targetVersionId).toBe(TARGET_ID);
  });
});

describe('getDiff — errors pass-through', () => {
  it('404 DIFF_NOT_FOUND прокидывается (используется retry-predicate)', async () => {
    const err = new OrchestratorError({
      error_code: 'DIFF_NOT_FOUND',
      message: 'Сравнение ещё не готово',
      status: 404,
    });
    getSpy.mockRejectedValueOnce(err);
    await expect(
      getDiff({
        contractId: CONTRACT_ID,
        baseVersionId: BASE_ID,
        targetVersionId: TARGET_ID,
      }),
    ).rejects.toMatchObject({ error_code: 'DIFF_NOT_FOUND', status: 404 });
  });

  it.each([
    ['VERSION_NOT_FOUND', 404],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    getSpy.mockRejectedValueOnce(err);
    await expect(
      getDiff({
        contractId: CONTRACT_ID,
        baseVersionId: BASE_ID,
        targetVersionId: TARGET_ID,
      }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
