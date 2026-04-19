// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет URL, 200-ответ и
// нормализацию ошибок через interceptor.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { archiveContract, archiveContractEndpoint } from './archive-contract';
import { __setHttpForTests } from './http';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'ARCHIVED' as const,
  current_version_number: 2,
  processing_status: 'READY' as const,
  created_at: '2026-04-01T10:00:00Z',
  updated_at: '2026-04-19T12:00:00Z',
};

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('archiveContract — integration с axios-client', () => {
  it('200 → ответ проксируется; POST без тела', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    let seenBodyBytes = 0;
    server.use(
      mswHttp.post(url(archiveContractEndpoint(CONTRACT_ID)), async ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        const raw = await request.arrayBuffer();
        seenBodyBytes = raw.byteLength;
        return HttpResponse.json(OK_RESPONSE, { status: 200 });
      }),
    );

    const result = await archiveContract({ contractId: CONTRACT_ID });

    expect(seenMethod).toBe('POST');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/archive`);
    // POST без body: axios не передаёт payload.
    expect(seenBodyBytes).toBe(0);
    expect(result.status).toBe('ARCHIVED');
  });

  it('409 DOCUMENT_ARCHIVED → OrchestratorError с status=409', async () => {
    server.use(
      mswHttp.post(url(archiveContractEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json(
          { error_code: 'DOCUMENT_ARCHIVED', message: 'Документ уже в архиве' },
          { status: 409 },
        ),
      ),
    );

    try {
      await archiveContract({ contractId: CONTRACT_ID });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('DOCUMENT_ARCHIVED');
      expect(err.status).toBe(409);
    }
  });

  it.each([
    ['DOCUMENT_NOT_FOUND', 404],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('%s (HTTP %d) → OrchestratorError', async (code, status) => {
    server.use(
      mswHttp.post(url(archiveContractEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json({ error_code: code, message: 'm' }, { status }),
      ),
    );

    try {
      await archiveContract({ contractId: CONTRACT_ID });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe(code);
      expect((e as OrchestratorError).status).toBe(status);
    }
  });
});
