// Unit-тесты api/archive-contract.ts: endpoint, отсутствие body, прокидывание
// ответа и ошибок.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { archiveContract, archiveContractEndpoint } from './archive-contract';
import { __setHttpForTests } from './http';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'ARCHIVED' as const,
  current_version_number: 2,
  processing_status: 'READY' as const,
  created_at: '2026-04-01T10:00:00Z',
  updated_at: '2026-04-19T12:00:00Z',
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

describe('archiveContract — call shape', () => {
  it('POST на /contracts/{id}/archive БЕЗ тела', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await archiveContract({ contractId: CONTRACT_ID });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(archiveContractEndpoint(CONTRACT_ID));
    expect(path).toBe(`/contracts/${CONTRACT_ID}/archive`);
    expect(body).toBeUndefined();
  });

  it('contract_id экранируется', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, contract_id: 'a/b' } });
    await archiveContract({ contractId: 'a/b' });
    const [path] = postSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb/archive');
  });

  it('signal передаётся в config, когда задан', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    await archiveContract({ contractId: CONTRACT_ID }, { signal: controller.signal });
    const [, , config] = postSpy.mock.calls[0]! as [string, unknown, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });
});

describe('archiveContract — response', () => {
  it('возвращает ContractSummary как есть (snake_case)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await archiveContract({ contractId: CONTRACT_ID });
    expect(result).toEqual(OK_RESPONSE);
    expect(result.status).toBe('ARCHIVED');
  });
});

describe('archiveContract — errors pass-through', () => {
  it.each([
    ['DOCUMENT_ARCHIVED', 409],
    ['DOCUMENT_NOT_FOUND', 404],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    postSpy.mockRejectedValueOnce(err);
    await expect(archiveContract({ contractId: CONTRACT_ID })).rejects.toMatchObject({
      error_code: code,
      status,
    });
  });
});
