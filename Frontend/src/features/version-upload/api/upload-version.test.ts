// Unit-тесты api/upload-version.ts: контракт вызова axios (endpoint с
// контекстным contractId, FormData только с `file`, options) и нормализация
// ответа. HTTP-инстанс подменяется через `__setHttpForTests` на минимальный
// мок — изолируемся от transformRequest axios'а.
import type { AxiosInstance, AxiosProgressEvent, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { __setHttpForTests } from './http';
import { uploadVersion, uploadVersionEndpoint } from './upload-version';

const OK_RESPONSE = {
  contract_id: 'c0ffee00-1111-2222-3333-444444444444',
  version_id: 'v2ee0000-aaaa-bbbb-cccc-dddddddddddd',
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'UPLOADED' as const,
  message: 'Новая версия принята в обработку',
};

const CONTRACT_ID = OK_RESPONSE.contract_id;

function makeFile(name = 'v2.pdf', size = 1024, type = 'application/pdf'): File {
  const bytes = new Uint8Array(size);
  bytes[0] = 0x25;
  return new File([bytes], name, { type });
}

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

describe('uploadVersion — call shape', () => {
  it('POST на /contracts/{id}/versions/upload с FormData(file)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await uploadVersion({ contractId: CONTRACT_ID, file: makeFile('v2.pdf', 100) });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(uploadVersionEndpoint(CONTRACT_ID));
    expect(path).toBe(`/contracts/${CONTRACT_ID}/versions/upload`);
    expect(body).toBeInstanceOf(FormData);
    const fd = body as FormData;
    const file = fd.get('file');
    expect(file).toBeInstanceOf(File);
    expect((file as File).name).toBe('v2.pdf');
    // title НЕ должен присутствовать (в отличие от /contracts/upload).
    expect(fd.get('title')).toBeNull();
  });

  it('contractId экранируется в path', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, contract_id: 'a/b' } });
    await uploadVersion({ contractId: 'a/b', file: makeFile() });
    const [path] = postSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb/versions/upload');
  });

  it('timeout=120_000 передан в config', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await uploadVersion({ contractId: CONTRACT_ID, file: makeFile() });
    const [, , config] = postSpy.mock.calls[0]! as [string, FormData, AxiosRequestConfig];
    expect(config.timeout).toBe(120_000);
  });

  it('signal и onUploadProgress передаются, когда заданы', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    const onUploadProgress = vi.fn();
    await uploadVersion(
      { contractId: CONTRACT_ID, file: makeFile() },
      { signal: controller.signal, onUploadProgress },
    );
    const [, , config] = postSpy.mock.calls[0]! as [string, FormData, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
    expect(typeof config.onUploadProgress).toBe('function');
  });

  it('signal/onUploadProgress НЕ добавляются при отсутствии в opts', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await uploadVersion({ contractId: CONTRACT_ID, file: makeFile() });
    const [, , config] = postSpy.mock.calls[0]! as [string, FormData, AxiosRequestConfig];
    expect(config.signal).toBeUndefined();
    expect(config.onUploadProgress).toBeUndefined();
  });
});

describe('uploadVersion — onUploadProgress bridging', () => {
  it('loaded+total → fraction∈(0,1]', async () => {
    postSpy.mockImplementationOnce(async (_url, _fd, config: AxiosRequestConfig) => {
      const cb = config.onUploadProgress;
      cb?.({ loaded: 500, total: 1000, bytes: 500 } as unknown as AxiosProgressEvent);
      cb?.({ loaded: 1000, total: 1000, bytes: 500 } as unknown as AxiosProgressEvent);
      return { data: OK_RESPONSE };
    });

    const events: Array<{ loaded: number; total?: number; fraction?: number }> = [];
    await uploadVersion(
      { contractId: CONTRACT_ID, file: makeFile() },
      { onUploadProgress: (p) => events.push({ ...p }) },
    );

    expect(events).toEqual([
      { loaded: 500, total: 1000, fraction: 0.5 },
      { loaded: 1000, total: 1000, fraction: 1 },
    ]);
  });

  it('total отсутствует → fraction undefined', async () => {
    postSpy.mockImplementationOnce(async (_url, _fd, config: AxiosRequestConfig) => {
      config.onUploadProgress?.({ loaded: 400, bytes: 400 } as unknown as AxiosProgressEvent);
      return { data: OK_RESPONSE };
    });
    const events: Array<{ loaded: number; total?: number; fraction?: number }> = [];
    await uploadVersion(
      { contractId: CONTRACT_ID, file: makeFile() },
      { onUploadProgress: (p) => events.push({ ...p }) },
    );
    expect(events).toEqual([{ loaded: 400 }]);
  });
});

describe('uploadVersion — response narrow', () => {
  it('camelCase narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await uploadVersion({ contractId: CONTRACT_ID, file: makeFile() });
    expect(result).toEqual({
      contractId: OK_RESPONSE.contract_id,
      versionId: OK_RESPONSE.version_id,
      versionNumber: 2,
      jobId: OK_RESPONSE.job_id,
      status: 'UPLOADED',
      message: 'Новая версия принята в обработку',
    });
  });

  it('message отсутствует → не включается в результат', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, message: undefined } });
    const result = await uploadVersion({ contractId: CONTRACT_ID, file: makeFile() });
    expect(result).not.toHaveProperty('message');
  });

  it('отсутствие version_id в 202 → throw (спецификационный drift)', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, version_id: undefined } });
    await expect(
      uploadVersion({ contractId: CONTRACT_ID, file: makeFile() }),
    ).rejects.toThrow(/UploadResponse/);
  });
});

describe('uploadVersion — errors pass-through', () => {
  it.each([
    ['FILE_TOO_LARGE', 413],
    ['UNSUPPORTED_FORMAT', 415],
    ['INVALID_FILE', 400],
    ['DOCUMENT_NOT_FOUND', 404],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    postSpy.mockRejectedValueOnce(err);
    await expect(
      uploadVersion({ contractId: CONTRACT_ID, file: makeFile() }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
