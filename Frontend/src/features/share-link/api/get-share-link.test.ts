// Unit-тесты api/get-share-link.ts: контракт axios-вызова, извлечение Location,
// нормализация 302-без-Location, pass-through 403/404.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { AxiosHeaders } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { getShareLink, shareLinkEndpoint } from './get-share-link';
import { __setHttpForTests } from './http';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED = 'https://presigned.example/contractpro/report.docx?X-Expires=300';

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

describe('getShareLink — call shape', () => {
  it('GET на тот же export-endpoint', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    await getShareLink({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'docx',
    });

    const [path, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(path).toBe(
      shareLinkEndpoint({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'docx',
      }),
    );
    expect(path).toBe(`/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/export/docx`);
    expect(config.maxRedirects).toBe(0);
    expect(config.validateStatus?.(302)).toBe(true);
  });

  it('signal передаётся в config, когда задан', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    const controller = new AbortController();
    await getShareLink(
      { contractId: CONTRACT_ID, versionId: VERSION_ID, format: 'pdf' },
      { signal: controller.signal },
    );
    const [, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });
});

describe('getShareLink — response parsing', () => {
  it('извлекает Location из plain-object headers', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    const result = await getShareLink({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'docx',
    });
    expect(result.location).toBe(PRESIGNED);
  });

  it('извлекает Location из AxiosHeaders', async () => {
    const headers = new AxiosHeaders();
    headers.set('Location', PRESIGNED);
    getSpy.mockResolvedValueOnce({ status: 302, headers });
    const result = await getShareLink({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'pdf',
    });
    expect(result.location).toBe(PRESIGNED);
  });

  it('302 без Location → OrchestratorError INTERNAL_ERROR', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: {} });
    await expect(
      getShareLink({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'pdf',
      }),
    ).rejects.toBeInstanceOf(OrchestratorError);
  });
});

describe('getShareLink — errors pass-through', () => {
  it.each([
    ['PERMISSION_DENIED', 403],
    ['ARTIFACT_NOT_FOUND', 404],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    getSpy.mockRejectedValueOnce(err);
    await expect(
      getShareLink({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'pdf',
      }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
