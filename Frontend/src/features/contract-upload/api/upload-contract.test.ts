// Unit-тесты api/upload-contract.ts: проверяют контракт вызова axios
// (endpoint, FormData, options) и нормализацию ответа. HTTP-инстанс подменяется
// через `__setHttpForTests` на минимальный мок — это даёт:
//   • детерминированный контроль над FormData, onUploadProgress-callback'ом;
//   • изоляцию от transformRequest axios'а (axios 1.x сериализует web-FormData
//     в urlencoded при пустом Content-Type в node — мешает MSW FormData-parse'у).
// Интеграционные проверки реальной axios-транспортировки + error normalization
// см. в `upload-contract.integration.test.ts` и существующем `shared/api/client.test.ts`.
import type { AxiosInstance, AxiosProgressEvent, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { __setHttpForTests } from './http';
import { UPLOAD_CONTRACT_ENDPOINT,uploadContract } from './upload-contract';

const OK_RESPONSE = {
  contract_id: 'c0ffee00-1111-2222-3333-444444444444',
  version_id: 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd',
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'UPLOADED' as const,
  message: 'Договор принят в обработку',
};

function makeFile(name = 'contract.pdf', size = 1024, type = 'application/pdf'): File {
  const bytes = new Uint8Array(size);
  bytes[0] = 0x25;
  return new File([bytes], name, { type });
}

type MockPost = ReturnType<typeof vi.fn>;

function mockHttp(post: MockPost): AxiosInstance {
  // Узкая утка AxiosInstance: uploadContract использует только `post`.
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

describe('uploadContract — call shape', () => {
  it('POST на правильный endpoint с FormData(file, title)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await uploadContract({ file: makeFile('c.pdf', 100), title: 'Договор 1' });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(UPLOAD_CONTRACT_ENDPOINT);
    expect(body).toBeInstanceOf(FormData);
    const fd = body as FormData;
    const file = fd.get('file');
    expect(file).toBeInstanceOf(File);
    expect((file as File).name).toBe('c.pdf');
    expect(fd.get('title')).toBe('Договор 1');
  });

  it('timeout=120_000 передан в config', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await uploadContract({ file: makeFile(), title: 't' });
    const [, , config] = postSpy.mock.calls[0]! as [string, FormData, AxiosRequestConfig];
    expect(config.timeout).toBe(120_000);
  });

  it('signal и onUploadProgress передаются в config, когда заданы', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    const onUploadProgress = vi.fn();
    await uploadContract(
      { file: makeFile(), title: 't' },
      { signal: controller.signal, onUploadProgress },
    );
    const [, , config] = postSpy.mock.calls[0]! as [string, FormData, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
    expect(typeof config.onUploadProgress).toBe('function');
  });

  it('signal/onUploadProgress НЕ добавляются в config при отсутствии в opts', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await uploadContract({ file: makeFile(), title: 't' });
    const [, , config] = postSpy.mock.calls[0]! as [string, FormData, AxiosRequestConfig];
    expect(config.signal).toBeUndefined();
    expect(config.onUploadProgress).toBeUndefined();
  });
});

describe('uploadContract — onUploadProgress bridging', () => {
  it('loaded+total → fraction∈(0,1]', async () => {
    postSpy.mockImplementationOnce(async (_url, _fd, config: AxiosRequestConfig) => {
      const cb = config.onUploadProgress;
      cb?.({ loaded: 500, total: 1000, bytes: 500 } as unknown as AxiosProgressEvent);
      cb?.({ loaded: 1000, total: 1000, bytes: 500 } as unknown as AxiosProgressEvent);
      return { data: OK_RESPONSE };
    });

    const events: Array<{ loaded: number; total?: number; fraction?: number }> = [];
    await uploadContract(
      { file: makeFile(), title: 't' },
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
    await uploadContract(
      { file: makeFile(), title: 't' },
      { onUploadProgress: (p) => events.push({ ...p }) },
    );
    expect(events).toEqual([{ loaded: 400 }]);
  });

  it('total=0 трактуется как неизвестный (fraction undefined, избегаем деления на 0)', async () => {
    postSpy.mockImplementationOnce(async (_url, _fd, config: AxiosRequestConfig) => {
      config.onUploadProgress?.({ loaded: 123, total: 0 } as unknown as AxiosProgressEvent);
      return { data: OK_RESPONSE };
    });
    const events: Array<{ loaded: number; total?: number; fraction?: number }> = [];
    await uploadContract(
      { file: makeFile(), title: 't' },
      { onUploadProgress: (p) => events.push({ ...p }) },
    );
    expect(events).toEqual([{ loaded: 123 }]);
  });
});

describe('uploadContract — response narrow', () => {
  it('camelCase narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await uploadContract({ file: makeFile(), title: 't' });
    expect(result).toEqual({
      contractId: OK_RESPONSE.contract_id,
      versionId: OK_RESPONSE.version_id,
      versionNumber: 2,
      jobId: OK_RESPONSE.job_id,
      status: 'UPLOADED',
      message: 'Договор принят в обработку',
    });
  });

  it('message отсутствует → не включается в результат', async () => {
    postSpy.mockResolvedValueOnce({
      data: { ...OK_RESPONSE, message: undefined },
    });
    const result = await uploadContract({ file: makeFile(), title: 't' });
    expect(result).not.toHaveProperty('message');
  });

  it('отсутствие contract_id в 202 → throw (спецификационный drift)', async () => {
    postSpy.mockResolvedValueOnce({ data: { ...OK_RESPONSE, contract_id: undefined } });
    await expect(uploadContract({ file: makeFile(), title: 't' })).rejects.toThrow(
      /UploadResponse/,
    );
  });
});

describe('uploadContract — errors pass-through', () => {
  it('OrchestratorError от interceptor прокидывается as-is', async () => {
    const err = new OrchestratorError({
      error_code: 'FILE_TOO_LARGE',
      message: 'Файл больше 20 МБ',
      status: 413,
    });
    postSpy.mockRejectedValueOnce(err);
    await expect(uploadContract({ file: makeFile(), title: 't' })).rejects.toBe(err);
  });

  it.each([
    ['UNSUPPORTED_FORMAT', 415],
    ['INVALID_FILE', 400],
    ['VALIDATION_ERROR', 400],
    ['INTERNAL_ERROR', 500],
    ['NETWORK_ERROR', undefined],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError(
      status !== undefined
        ? { error_code: code, message: 'm', status }
        : { error_code: code, message: 'm' },
    );
    postSpy.mockRejectedValueOnce(err);
    await expect(uploadContract({ file: makeFile(), title: 't' })).rejects.toMatchObject({
      error_code: code,
      ...(status !== undefined && { status }),
    });
  });
});
