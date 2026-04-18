// Unit-тесты api/export-report.ts: контракт axios-вызова (endpoint,
// maxRedirects=0, validateStatus=302, signal), чтение Location из разных
// форматов headers, нормализация 302-без-Location в OrchestratorError,
// pass-through 403/404.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { AxiosHeaders } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import { exportReport, exportReportEndpoint } from './export-report';
import { __setHttpForTests } from './http';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED = 'https://presigned.example/contractpro/report.pdf?X-Expires=300';

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

describe('exportReport — call shape', () => {
  it('GET на /contracts/{id}/versions/{vid}/export/{format}', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    await exportReport({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'pdf',
    });

    expect(getSpy).toHaveBeenCalledTimes(1);
    const [path, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(path).toBe(
      exportReportEndpoint({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'pdf',
      }),
    );
    expect(path).toBe(`/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/export/pdf`);
    expect(config.maxRedirects).toBe(0);
    expect(typeof config.validateStatus).toBe('function');
    expect(config.validateStatus?.(302)).toBe(true);
    expect(config.validateStatus?.(200)).toBe(false);
    expect(config.validateStatus?.(404)).toBe(false);
  });

  it('все path-параметры экранируются', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    await exportReport({
      contractId: 'a/b',
      versionId: 'v#1',
      format: 'docx',
    });
    const [path] = getSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb/versions/v%231/export/docx');
  });

  it('signal передаётся в config, когда задан', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    const controller = new AbortController();
    await exportReport(
      { contractId: CONTRACT_ID, versionId: VERSION_ID, format: 'pdf' },
      { signal: controller.signal },
    );
    const [, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });

  it('без signal — ключ signal отсутствует в config', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    await exportReport({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'pdf',
    });
    const [, config] = getSpy.mock.calls[0]! as [string, AxiosRequestConfig];
    expect('signal' in (config ?? {})).toBe(false);
  });
});

describe('exportReport — response parsing', () => {
  it('извлекает Location из plain-object headers', async () => {
    getSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED },
    });
    const result = await exportReport({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'pdf',
    });
    expect(result.location).toBe(PRESIGNED);
  });

  it('извлекает Location из AxiosHeaders (через .get)', async () => {
    const headers = new AxiosHeaders();
    headers.set('Location', PRESIGNED);
    getSpy.mockResolvedValueOnce({ status: 302, headers });

    const result = await exportReport({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      format: 'pdf',
    });
    expect(result.location).toBe(PRESIGNED);
  });

  it('302 без Location → OrchestratorError INTERNAL_ERROR', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: {} });
    await expect(
      exportReport({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'pdf',
      }),
    ).rejects.toMatchObject({ error_code: 'INTERNAL_ERROR' });
  });

  it('302 с headers=null → OrchestratorError INTERNAL_ERROR', async () => {
    getSpy.mockResolvedValueOnce({ status: 302, headers: null });
    await expect(
      exportReport({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'pdf',
      }),
    ).rejects.toBeInstanceOf(OrchestratorError);
  });
});

describe('exportReport — errors pass-through', () => {
  it.each([
    ['PERMISSION_DENIED', 403],
    ['ARTIFACT_NOT_FOUND', 404],
    ['RESULTS_NOT_READY', 404],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    getSpy.mockRejectedValueOnce(err);
    await expect(
      exportReport({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        format: 'pdf',
      }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
