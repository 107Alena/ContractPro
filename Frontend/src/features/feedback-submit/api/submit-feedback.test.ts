// Unit-тесты api/submit-feedback.ts: endpoint, сериализация тела,
// нормализация 201, прокидывание ошибок.
import type { AxiosInstance, AxiosRequestConfig } from 'axios';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api/errors';

import { __setHttpForTests } from './http';
import { submitFeedback, submitFeedbackEndpoint } from './submit-feedback';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd';
const FEEDBACK_ID = 'fb000000-5555-6666-7777-888888888888';
const CREATED_AT = '2026-04-19T10:00:00Z';

const OK_RESPONSE = {
  feedback_id: FEEDBACK_ID,
  created_at: CREATED_AT,
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

describe('submitFeedback — call shape', () => {
  it('POST на /contracts/{id}/versions/{vid}/feedback с JSON-телом', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await submitFeedback({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      isUseful: true,
      comment: 'Полезный отчёт',
    });

    expect(postSpy).toHaveBeenCalledTimes(1);
    const [path, body] = postSpy.mock.calls[0]!;
    expect(path).toBe(submitFeedbackEndpoint(CONTRACT_ID, VERSION_ID));
    expect(path).toBe(`/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/feedback`);
    expect(body).toEqual({ is_useful: true, comment: 'Полезный отчёт' });
  });

  it('comment опционален — не включается в тело, если undefined', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await submitFeedback({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      isUseful: false,
    });
    const [, body] = postSpy.mock.calls[0]!;
    expect(body).toEqual({ is_useful: false });
    expect(body).not.toHaveProperty('comment');
  });

  it('path-параметры экранируются', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    await submitFeedback({
      contractId: 'a/b',
      versionId: 'v#1',
      isUseful: true,
    });
    const [path] = postSpy.mock.calls[0]!;
    expect(path).toBe('/contracts/a%2Fb/versions/v%231/feedback');
  });

  it('signal передаётся в config, когда задан', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const controller = new AbortController();
    await submitFeedback(
      { contractId: CONTRACT_ID, versionId: VERSION_ID, isUseful: true },
      { signal: controller.signal },
    );
    const [, , config] = postSpy.mock.calls[0]! as [string, unknown, AxiosRequestConfig];
    expect(config.signal).toBe(controller.signal);
  });
});

describe('submitFeedback — response narrow', () => {
  it('camelCase narrowed-response', async () => {
    postSpy.mockResolvedValueOnce({ data: OK_RESPONSE });
    const result = await submitFeedback({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      isUseful: true,
    });
    expect(result).toEqual({ feedbackId: FEEDBACK_ID, createdAt: CREATED_AT });
  });

  it('отсутствие feedback_id → throw', async () => {
    postSpy.mockResolvedValueOnce({ data: { created_at: CREATED_AT } });
    await expect(
      submitFeedback({ contractId: CONTRACT_ID, versionId: VERSION_ID, isUseful: true }),
    ).rejects.toThrow(/FeedbackResponse/);
  });

  it('отсутствие created_at → throw', async () => {
    postSpy.mockResolvedValueOnce({ data: { feedback_id: FEEDBACK_ID } });
    await expect(
      submitFeedback({ contractId: CONTRACT_ID, versionId: VERSION_ID, isUseful: true }),
    ).rejects.toThrow(/FeedbackResponse/);
  });
});

describe('submitFeedback — errors pass-through', () => {
  it.each([
    ['VALIDATION_ERROR', 400],
    ['AUTH_TOKEN_EXPIRED', 401],
    ['VERSION_NOT_FOUND', 404],
    ['INTERNAL_ERROR', 500],
  ])('код %s / status %s прокидывается', async (code, status) => {
    const err = new OrchestratorError({ error_code: code, message: 'm', status });
    postSpy.mockRejectedValueOnce(err);
    await expect(
      submitFeedback({ contractId: CONTRACT_ID, versionId: VERSION_ID, isUseful: true }),
    ).rejects.toMatchObject({ error_code: code, status });
  });
});
