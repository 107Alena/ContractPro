// @vitest-environment node
// Integration-тест: реальный axios-client (fetch-adapter) + MSW.
// Проверяет endpoint + 302→Location extraction + 403 PERMISSION_DENIED.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { getShareLink, shareLinkEndpoint } from './get-share-link';
import { __setHttpForTests } from './http';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED_DOCX = 'https://presigned.example/contractpro/report.docx?X-Expires=300';

const INPUT = {
  contractId: CONTRACT_ID,
  versionId: VERSION_ID,
  format: 'docx' as const,
};

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('getShareLink — integration с axios-client', () => {
  it('302 + Location → {location}; path /contracts/{id}/versions/{vid}/export/docx', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    server.use(
      mswHttp.get(url(shareLinkEndpoint(INPUT)), ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        return new HttpResponse(null, {
          status: 302,
          headers: { Location: PRESIGNED_DOCX },
        });
      }),
    );

    const result = await getShareLink(INPUT);

    expect(seenMethod).toBe('GET');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/export/docx`);
    expect(result.location).toBe(PRESIGNED_DOCX);
  });

  it('403 PERMISSION_DENIED → OrchestratorError', async () => {
    server.use(
      mswHttp.get(url(shareLinkEndpoint(INPUT)), () =>
        HttpResponse.json(
          { error_code: 'PERMISSION_DENIED', message: 'Экспорт запрещён' },
          { status: 403 },
        ),
      ),
    );

    try {
      await getShareLink(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe('PERMISSION_DENIED');
      expect((e as OrchestratorError).status).toBe(403);
    }
  });

  it('404 ARTIFACT_NOT_FOUND → OrchestratorError', async () => {
    server.use(
      mswHttp.get(url(shareLinkEndpoint(INPUT)), () =>
        HttpResponse.json(
          { error_code: 'ARTIFACT_NOT_FOUND', message: 'Отчёт ещё не готов' },
          { status: 404 },
        ),
      ),
    );

    try {
      await getShareLink(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe('ARTIFACT_NOT_FOUND');
    }
  });
});
