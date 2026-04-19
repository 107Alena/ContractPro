// Unit-тесты api/delete-contract.ts: endpoint, DELETE-метод, прокидывание
// ответа/ошибок.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { deleteContract, deleteContractEndpoint } from './delete-contract';
import { __setHttpForTests } from './http';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'DELETED' as const,
  current_version_number: 2,
  created_at: '2026-04-01T10:00:00Z',
  updated_at: '2026-04-19T12:00:00Z',
};

type MockDelete = ReturnType<typeof vi.fn>;

function mockHttp(del: MockDelete): AxiosInstance {
  return { delete: del } as unknown as AxiosInstance;
}

let deleteSpy: MockDelete;

beforeEach(() => {
  deleteSpy = vi.fn();
  __setHttpForTests(mockHttp(deleteSpy));
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('deleteContract — call shape', () => {
  it('DELETE на /contracts/{id}', async () => {
    deleteSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await deleteContract({ contractId: CONTRACT_ID });

    expect(deleteSpy).toHaveBeenCalledTimes(1);
    const [path] = deleteSpy.mock.calls[0]!;
    expect(path).toBe(deleteContractEndpoint(CONTRACT_ID));
    expect(path).toBe(`/contracts/${CONTRACT_ID}`);
  });

  it('contract_id экранируется', async () => {
    deleteSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, contract_id: 'a/b' } });
    await deleteContract({ contractId: 'a/b' });
    const [path] = deleteSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb');
  });

  it('signal передаётся в config', async () => {
    deleteSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    await deleteContract({ contractId: CONTRACT_ID }, { signal: controller.signal });
    const [, config] = deleteSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });
});

describe('deleteContract — response', () => {
  it('возвращает ContractSummary со status=DELETED', async () => {
    deleteSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await deleteContract({ contractId: CONTRACT_ID });
    expect(result).toEqual(OK_RESPONSE);
    expect(result.status).toBe('DELETED');
  });
});

describe('deleteContract — errors pass-through', () => {
  it.each([
    ['DOCUMENT_NOT_FOUND', 404],
    ['DOCUMENT_DELETED', 409],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    deleteSpy.mockRejectedValueOnce(err);
    await expect(deleteContract({ contractId: CONTRACT_ID })).rejects.toMatchObject({
      error_code: code,
      status,
    });
  });
});
