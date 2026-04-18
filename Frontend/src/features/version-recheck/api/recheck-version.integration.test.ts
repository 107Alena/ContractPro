// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет endpoint,
// 202 narrow и нормализацию 409 VERSION_STILL_PROCESSING.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { __setHttpForTests } from './http';
import { recheckVersion, recheckVersionEndpoint } from './recheck-version';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  version_id: VERSION_ID,
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'QUEUED' as const,
};

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('recheckVersion — integration с axios-client', () => {
  it('202 → narrowed response; path /contracts/{id}/versions/{vid}/recheck', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    server.use(
      mswHttp.post(url(recheckVersionEndpoint(CONTRACT_ID, VERSION_ID)), ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        return HttpResponse.json(OK_RESPONSE, { status: 202 });
      }),
    );

    const result = await recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID });

    expect(seenMethod).toBe('POST');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/recheck`);
    expect(result.status).toBe('QUEUED');
    expect(result.versionNumber).toBe(2);
  });

  it('409 VERSION_STILL_PROCESSING → OrchestratorError с status=409', async () => {
    server.use(
      mswHttp.post(url(recheckVersionEndpoint(CONTRACT_ID, VERSION_ID)), () =>
        HttpResponse.json(
          {
            error_code: 'VERSION_STILL_PROCESSING',
            message: 'Версия ещё обрабатывается',
          },
          { status: 409 },
        ),
      ),
    );

    try {
      await recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('VERSION_STILL_PROCESSING');
      expect(err.status).toBe(409);
      expect(err.message).toBe('Версия ещё обрабатывается');
    }
  });

  it.each([
    ['VERSION_NOT_FOUND', 404],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('%s (HTTP %d) → OrchestratorError', async (code, status) => {
    server.use(
      mswHttp.post(url(recheckVersionEndpoint(CONTRACT_ID, VERSION_ID)), () =>
        HttpResponse.json({ error_code: code, message: 'm' }, { status }),
      ),
    );

    try {
      await recheckVersion({ contractId: CONTRACT_ID, versionId: VERSION_ID });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe(code);
      expect((e as OrchestratorError).status).toBe(status);
    }
  });
});
