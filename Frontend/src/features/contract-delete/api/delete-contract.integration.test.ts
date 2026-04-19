// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет DELETE-URL,
// 200-ответ и нормализацию ошибок.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { deleteContract, deleteContractEndpoint } from './delete-contract';
import { __setHttpForTests } from './http';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';

const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  title: 'Договор №1',
  status: 'DELETED' as const,
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

describe('deleteContract — integration с axios-client', () => {
  it('200 → ответ проксируется; DELETE-метод', async () => {
    let seenMethod: string | undefined;
    let seenPath: string | undefined;
    server.use(
      mswHttp.delete(url(deleteContractEndpoint(CONTRACT_ID)), ({ request }) => {
        seenMethod = request.method;
        seenPath = new URL(request.url).pathname;
        return HttpResponse.json(OK_RESPONSE, { status: 200 });
      }),
    );

    const result = await deleteContract({ contractId: CONTRACT_ID });

    expect(seenMethod).toBe('DELETE');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}`);
    expect(result.status).toBe('DELETED');
  });

  it('409 DOCUMENT_DELETED → OrchestratorError с status=409', async () => {
    server.use(
      mswHttp.delete(url(deleteContractEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json(
          { error_code: 'DOCUMENT_DELETED', message: 'Уже удалён' },
          { status: 409 },
        ),
      ),
    );

    try {
      await deleteContract({ contractId: CONTRACT_ID });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe('DOCUMENT_DELETED');
      expect((e as OrchestratorError).status).toBe(409);
    }
  });

  it.each([
    ['DOCUMENT_NOT_FOUND', 404],
    ['PERMISSION_DENIED', 403],
  ])('%s (HTTP %d) → OrchestratorError', async (code, status) => {
    server.use(
      mswHttp.delete(url(deleteContractEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json({ error_code: code, message: 'm' }, { status }),
      ),
    );

    try {
      await deleteContract({ contractId: CONTRACT_ID });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe(code);
      expect((e as OrchestratorError).status).toBe(status);
    }
  });
});
