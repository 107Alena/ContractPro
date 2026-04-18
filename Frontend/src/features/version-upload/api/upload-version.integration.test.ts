// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет, что upload-слой
// и shared/api/client.ts корректно интегрированы:
//   • POST /contracts/{id}/versions/upload идёт на правильный абсолютный URL;
//   • 202 → narrowed-response;
//   • 413/415/400/409 нормализуются как OrchestratorError (контракт §7.2).
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { __setHttpForTests } from './http';
import { uploadVersion, uploadVersionEndpoint } from './upload-version';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const OK_RESPONSE = {
  contract_id: CONTRACT_ID,
  version_id: 'v2ee0000-aaaa-bbbb-cccc-dddddddddddd',
  version_number: 2,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'UPLOADED' as const,
};

function makeFile(): File {
  return new File([new Uint8Array([0x25, 0x50, 0x44, 0x46])], 'v2.pdf', {
    type: 'application/pdf',
  });
}

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  __setHttpForTests(null);
  __resetForTests();
});

describe('uploadVersion — integration с axios-client', () => {
  it('202 → narrowed response', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    server.use(
      mswHttp.post(url(uploadVersionEndpoint(CONTRACT_ID)), ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        return HttpResponse.json(OK_RESPONSE, { status: 202 });
      }),
    );

    const result = await uploadVersion({ contractId: CONTRACT_ID, file: makeFile() });

    expect(seenMethod).toBe('POST');
    expect(seenPath).toBe(`/api/v1/contracts/${CONTRACT_ID}/versions/upload`);
    expect(result.versionId).toBe(OK_RESPONSE.version_id);
    expect(result.versionNumber).toBe(2);
  });

  it.each([
    ['FILE_TOO_LARGE', 413, 'Файл больше 20 МБ'],
    ['UNSUPPORTED_FORMAT', 415, 'Поддерживается только PDF'],
    ['INVALID_FILE', 400, 'Файл повреждён'],
    ['DOCUMENT_NOT_FOUND', 404, 'Договор не найден'],
  ])('%s (HTTP %d) → OrchestratorError с error_code и status', async (code, status, msg) => {
    server.use(
      mswHttp.post(url(uploadVersionEndpoint(CONTRACT_ID)), () =>
        HttpResponse.json({ error_code: code, message: msg }, { status }),
      ),
    );

    try {
      await uploadVersion({ contractId: CONTRACT_ID, file: makeFile() });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe(code);
      expect(err.status).toBe(status);
    }
  });
});
