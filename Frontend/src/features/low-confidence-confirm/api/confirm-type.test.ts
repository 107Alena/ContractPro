import type { AxiosInstance } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { CONFIRM_TYPE_ENDPOINT, confirmType } from './confirm-type';
import { __setHttpForTests } from './http';

const OK_RESPONSE = {
  contract_id: 'c0ffee00-1111-2222-3333-444444444444',
  version_id: 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd',
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'ANALYZING' as const,
};

let postSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  postSpy = vi.fn();
  __setHttpForTests({ post: postSpy } as unknown as AxiosInstance);
});

afterEach(() => {
  __setHttpForTests(null);
});

describe('confirmType — request shape', () => {
  it('POST на корректный URL с обязательным body {contract_type, confirmed_by_user:true}', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });

    await confirmType({
      contractId: 'c0ffee00-1111-2222-3333-444444444444',
      versionId: 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd',
      contractType: 'услуги',
    });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(
      '/contracts/c0ffee00-1111-2222-3333-444444444444/versions/v1ee0000-aaaa-bbbb-cccc-dddddddddddd/confirm-type',
    );
    expect(body).toEqual({ contract_type: 'услуги', confirmed_by_user: true });
  });

  it('CONFIRM_TYPE_ENDPOINT helper делает encodeURIComponent для path-параметров', () => {
    expect(CONFIRM_TYPE_ENDPOINT('c/d', 'v?x')).toBe(
      '/contracts/c%2Fd/versions/v%3Fx/confirm-type',
    );
  });

  it('signal пробрасывается в axios config', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();

    await confirmType({
      contractId: 'cid',
      versionId: 'vid',
      contractType: 'услуги',
      signal: controller.signal,
    });

    const cfg = postSpy.mock.calls[0]![2];
    expect(cfg.signal).toBe(controller.signal);
  });
});

describe('confirmType — response narrowing', () => {
  it('202 валидный → narrowed-response (camelCase)', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });

    const result = await confirmType({ contractId: 'cid', versionId: 'vid', contractType: 'NDA' });

    expect(result).toEqual({
      contractId: OK_RESPONSE.contract_id,
      versionId: OK_RESPONSE.version_id,
      status: OK_RESPONSE.status,
    });
  });

  it('contract_id отсутствует в ответе → fallback на input.contractId', async () => {
    postSpy.mockResolvedValueOnce({
      data: {
        version_id: 'vid-from-server',
        status: 'ANALYZING',
      },
    });

    const result = await confirmType({
      contractId: 'fallback-cid',
      versionId: 'vid-input',
      contractType: 'NDA',
    });

    expect(result.contractId).toBe('fallback-cid');
    expect(result.versionId).toBe('vid-from-server');
  });

  it('version_id или status отсутствуют → throw (защита от спецификационного drift)', async () => {
    postSpy.mockResolvedValueOnce({ data: { contract_id: 'cid' } });
    await expect(
      confirmType({ contractId: 'cid', versionId: 'vid', contractType: 'NDA' }),
    ).rejects.toThrow('ConfirmTypeResponse');
  });
});
