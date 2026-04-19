// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет endpoint, 201 narrow
// и нормализацию ошибок через interceptor.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { __setHttpForTests } from './http';
import { submitFeedback, submitFeedbackEndpoint } from './submit-feedback';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd';
const FEEDBACK_ID = 'fb000000-5555-6666-7777-888888888888';
const CREATED_AT = '2026-04-19T10:00:00Z';

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('submitFeedback — integration с axios-client', () => {
  it('201 → narrowed response; path содержит contract_id + version_id', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    let seenBody: unknown;
    server.use(
      mswHttp.post(url(submitFeedbackEndpoint(CONTRACT_ID, VERSION_ID)), async ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        seenBody = await request.json();
        return HttpResponse.json(
          { feedback_id: FEEDBACK_ID, created_at: CREATED_AT },
          { status: 201 },
        );
      }),
    );

    const result = await submitFeedback({
      contractId: CONTRACT_ID,
      versionId: VERSION_ID,
      isUseful: true,
      comment: 'ок',
    });

    expect(seenMethod).toBe('POST');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/feedback`);
    expect(seenBody).toEqual({ is_useful: true, comment: 'ок' });
    expect(result).toEqual({ feedbackId: FEEDBACK_ID, createdAt: CREATED_AT });
  });

  it('400 VALIDATION_ERROR → OrchestratorError с status=400', async () => {
    server.use(
      mswHttp.post(url(submitFeedbackEndpoint(CONTRACT_ID, VERSION_ID)), () =>
        HttpResponse.json(
          { error_code: 'VALIDATION_ERROR', message: 'Неверное тело запроса' },
          { status: 400 },
        ),
      ),
    );

    try {
      await submitFeedback({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
      });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('VALIDATION_ERROR');
      expect(err.status).toBe(400);
    }
  });

  it.each([
    ['AUTH_TOKEN_EXPIRED', 401],
    ['VERSION_NOT_FOUND', 404],
  ])('%s (HTTP %d) → OrchestratorError', async (code, status) => {
    server.use(
      mswHttp.post(url(submitFeedbackEndpoint(CONTRACT_ID, VERSION_ID)), () =>
        HttpResponse.json({ error_code: code, message: 'm' }, { status }),
      ),
    );

    try {
      await submitFeedback({
        contractId: CONTRACT_ID,
        versionId: VERSION_ID,
        isUseful: true,
      });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe(code);
      expect((e as OrchestratorError).status).toBe(status);
    }
  });
});
