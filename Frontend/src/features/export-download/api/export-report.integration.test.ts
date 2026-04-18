// @vitest-environment node
// Integration-тест: реальный axios-client (fetch-adapter) + MSW.
// Проверяет endpoint, 302+Location narrow, 403 PERMISSION_DENIED.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { exportReport, exportReportEndpoint } from './export-report';
import { __setHttpForTests } from './http';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED_PDF = 'https://presigned.example/contractpro/report.pdf?X-Expires=300';

const INPUT = {
  contractId: CONTRACT_ID,
  versionId: VERSION_ID,
  format: 'pdf' as const,
};

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('exportReport — integration с axios-client', () => {
  it('302 + Location → {location}; path /contracts/{id}/versions/{vid}/export/pdf', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    server.use(
      mswHttp.get(url(exportReportEndpoint(INPUT)), ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        return new HttpResponse(null, {
          status: 302,
          headers: { Location: PRESIGNED_PDF },
        });
      }),
    );

    const result = await exportReport(INPUT);

    expect(seenMethod).toBe('GET');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/export/pdf`);
    expect(result.location).toBe(PRESIGNED_PDF);
  });

  it('403 PERMISSION_DENIED → OrchestratorError', async () => {
    server.use(
      mswHttp.get(url(exportReportEndpoint(INPUT)), () =>
        HttpResponse.json(
          { error_code: 'PERMISSION_DENIED', message: 'Недостаточно прав' },
          { status: 403 },
        ),
      ),
    );

    try {
      await exportReport(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('PERMISSION_DENIED');
      expect(err.status).toBe(403);
    }
  });

  it('404 ARTIFACT_NOT_FOUND → OrchestratorError', async () => {
    server.use(
      mswHttp.get(url(exportReportEndpoint(INPUT)), () =>
        HttpResponse.json(
          { error_code: 'ARTIFACT_NOT_FOUND', message: 'Отчёт ещё не готов' },
          { status: 404 },
        ),
      ),
    );

    try {
      await exportReport(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe('ARTIFACT_NOT_FOUND');
    }
  });
});
