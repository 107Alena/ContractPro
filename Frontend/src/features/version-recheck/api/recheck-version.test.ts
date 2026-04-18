// Unit-тесты api/recheck-version.ts: endpoint с двойным path-param,
// отсутствие body, нормализация ответа.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { __setHttpForTests } from './http';
import { recheckVersion, recheckVersionEndpoint } from './recheck-version';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  version_id: VERSION_ID,
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'QUEUED' as const,
  message: 'В очереди на повторную проверку',
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

describe('recheckVersion — call shape', () => {
  it('POST на /contracts/{id}/versions/{vid}/recheck БЕЗ тела', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(recheckVersionEndpoint(CONTRACT_ID, VERSION_ID));
    expect(path).toBe(`/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/recheck`);
    expect(body).toBeUndefined();
  });

  it('path-параметры экранируются', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, contract_id: 'a/b', version_id: 'v#1' } });
    await recheckVersion({ contractId: 'a/b', versionId: 'v#1' });
    const [path] = postSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb/versions/v%231/recheck');
  });

  it('signal передаётся в config, когда задан', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    await recheckVersion(
      { contractId: CONTRACT_ID, versionId: VERSION_ID },
      { signal: controller.signal },
    );
    const [, , config] = postSpy.mock.calls[0]! as [string, unknown, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });
});

describe('recheckVersion — response narrow', () => {
  it('camelCase narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID });
    expect(result).toEqual({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      versionNumber: 2,
      jobId: OK_RESPONSE.job_id,
      status: 'QUEUED',
      message: 'В очереди на повторную проверку',
    });
  });

  it('отсутствие обязательных полей → throw', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, job_id: undefined } });
    await expect(
      recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
    ).rejects.toThrow(/UploadResponse/);
  });
});

describe('recheckVersion — errors pass-through', () => {
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
      recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
