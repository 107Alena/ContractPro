// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет endpoint,
// 202 narrow и нормализацию 409 VERSION_STILL_PROCESSING / 404 VERSION_NOT_FOUND.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { __setHttpForTests } from './http';
import { startComparison, startComparisonEndpoint } from './start-comparison';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const BASE_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const TARGET_ID = 'ta39e700-aaaa-bbbb-cccc-222222222222';

const OK_RESPONSE = {
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'QUEUED' as const,
};

const INPUT = {
  contractId: CONTRACT_ID,
  baseVersionId: BASE_ID,
  targetVersionId: TARGET_ID,
};

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('startComparison — integration с axios-client', () => {
  it('202 → narrowed response; path /contracts/{id}/compare; body snake_case', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    let seenBody: unknown;
    server.use(
      mswHttp.post(url(startComparisonEndpoint(CONTRACT_ID)), async ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        seenBody = await request.json();
        return HttpResponse.json(OK_RESPONSE, { status: 202 });
      }),
    );

    const result = await startComparison(INPUT);

    expect(seenMethod).toBe('POST');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/compare`);
    expect(seenBody).toEqual({
      base_version_id: BASE_ID,
      target_version_id: TARGET_ID,
    });
    expect(result).toEqual({ jobId: OK_RESPONSE.job_id, status: 'QUEUED' });
  });

  it('409 VERSION_STILL_PROCESSING → OrchestratorError с status=409', async () => {
    server.use(
      mswHttp.post(url(startComparisonEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json(
          { error_code: 'VERSION_STILL_PROCESSING', message: 'Версия ещё обрабатывается' },
          { status: 409 },
        ),
      ),
    );

    try {
      await startComparison(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('VERSION_STILL_PROCESSING');
      expect(err.status).toBe(409);
    }
  });

  it.each([
    ['VERSION_NOT_FOUND', 404],
    ['DOCUMENT_ARCHIVED', 403],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('%s (HTTP %d) → OrchestratorError', async (code, status) => {
    server.use(
      mswHttp.post(url(startComparisonEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json({ error_code: code, message: 'm' }, { status }),
      ),
    );

    try {
      await startComparison(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe(code);
      expect((e as OrchestratorError).status).toBe(status);
    }
  });
});
