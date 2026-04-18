// Unit-тесты api/start-comparison.ts: контракт вызова axios (endpoint,
// snake_case body, options) и нормализация 202-ответа. HTTP-инстанс
// подменяется через `__setHttpForTests` на минимальный мок.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { __setHttpForTests } from './http';
import { startComparison, startComparisonEndpoint } from './start-comparison';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const BASE_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const TARGET_ID = 'ta39e700-aaaa-bbbb-cccc-222222222222';

const OK_RESPONSE = {
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'QUEUED' as const,
};

type MockPost = ReturnType<typeof vi.fn>;

function mockHttp(post: MockPost): AxiosInstance {
  return { post } as unknown as AxiosInstance;
}

let postSpy: MockPost;

beforeEach(() => {
  postSpy = vi.fn();
  __setHttpForTests(mockHttp(postSpy));
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('startComparison — call shape', () => {
  it('POST на /contracts/{id}/compare с snake_case body', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await startComparison({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(startComparisonEndpoint(CONTRACT_ID));
    expect(path).toBe(`/contracts/${CONTRACT_ID}/compare`);
    expect(body).toEqual({
      base_version_id: BASE_ID,
      target_version_id: TARGET_ID,
    });
  });

  it('contractId экранируется в path', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await startComparison({
      contractId: 'a/b#c',
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    const [path] = postSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb%23c/compare');
  });

  it('baseVersionId и targetVersionId попадают в body без изменений', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await startComparison({
      contractId: CONTRACT_ID,
      baseVersionId: 'with/slash',
      targetVersionId: 'with space',
    });
    const [, body] = postSpy.mock.calls[0]!;
    // Body идёт JSON-ом, axios сам сериализует; не нужно encodeURIComponent.
    expect(body).toEqual({
      base_version_id: 'with/slash',
      target_version_id: 'with space',
    });
  });

  it('signal передаётся в config, когда задан', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    await startComparison(
      { contractId: CONTRACT_ID, baseVersionId: BASE_ID, targetVersionId: TARGET_ID },
      { signal: controller.signal },
    );
    const [, , config] = postSpy.mock.calls[0]! as [string, unknown, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });

  it('без signal — ключ signal не попадает в config', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await startComparison({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    const [, , config] = postSpy.mock.calls[0]! as [string, unknown, AxiosRequestConfig];
    expect(config).toBeDefined();
    expect('signal' in (config ?? {})).toBe(false);
  });
});

describe('startComparison — response narrow', () => {
  it('camelCase narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await startComparison({
      contractId: CONTRACT_ID,
      baseVersionId: BASE_ID,
      targetVersionId: TARGET_ID,
    });
    expect(result).toEqual({
      jobId: OK_RESPONSE.job_id,
      status: 'QUEUED',
    });
  });

  it('отсутствие job_id → throw', async () => {
    postSpy.mockResolvedValueOnce({ data: { status: 'QUEUED' } });
    await expect(
      startComparison({
        contractId: CONTRACT_ID,
        baseVersionId: BASE_ID,
        targetVersionId: TARGET_ID,
      }),
    ).rejects.toThrow(/CompareResponse/);
  });

  it('отсутствие status → throw', async () => {
    postSpy.mockResolvedValueOnce({ data: { job_id: 'j' } });
    await expect(
      startComparison({
        contractId: CONTRACT_ID,
        baseVersionId: BASE_ID,
        targetVersionId: TARGET_ID,
      }),
    ).rejects.toThrow(/CompareResponse/);
  });

  it('job_id не string → throw', async () => {
    postSpy.mockResolvedValueOnce({ data: { job_id: 42, status: 'QUEUED' } });
    await expect(
      startComparison({
        contractId: CONTRACT_ID,
        baseVersionId: BASE_ID,
        targetVersionId: TARGET_ID,
      }),
    ).rejects.toThrow(/CompareResponse/);
  });
});

describe('startComparison — errors pass-through', () => {
  it.each([
    ['VERSION_STILL_PROCESSING', 409],
    ['VERSION_NOT_FOUND', 404],
    ['DOCUMENT_ARCHIVED', 403],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    postSpy.mockRejectedValueOnce(err);
    await expect(
      startComparison({
        contractId: CONTRACT_ID,
        baseVersionId: BASE_ID,
        targetVersionId: TARGET_ID,
      }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
