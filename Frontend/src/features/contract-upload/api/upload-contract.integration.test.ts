// Integration-тест: реальный axios-client + MSW. Проверяет, что upload-слой
// и shared/api/client.ts корректно интегрированы на границах:
//   • POST /contracts/upload идёт на правильный абсолютный URL (baseURL + path);
//   • 202 → narrowed-response;
//   • 413/415/400 INVALID_FILE/VALIDATION_ERROR/500 нормализуются как
//     OrchestratorError (контракт §7.2).
//
// Multipart-boundary не проверяем: axios 1.x в node сериализует FormData,
// но выставляет Content-Type=application/x-www-form-urlencoded (известное
// расхождение, см. upload-contract.test.ts для unit-проверки FormData-shape).
// MSW матчит по URL+method и отдаёт mock-response независимо от Content-Type.
//
// Environment: node (default). В jsdom global.fetch подменён jsdom-реализацией,
// которая не проходит через undici и MSW-interceptor не срабатывает.
import { http as mswHttp, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { __setHttpForTests } from './http';
import { UPLOAD_CONTRACT_ENDPOINT,uploadContract } from './upload-contract';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
// fetch-adapter шлёт через глобальный undici fetch → MSW v2 перехватывает;
// http-adapter axios'а в node FormData не форматирует корректно для interceptor'а.
testHttp.defaults.adapter = 'fetch';

const server = setupServer();

const OK_RESPONSE = {
  contract_id: 'c0ffee00-1111-2222-3333-444444444444',
  version_id: 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd',
  version_number: 1,
  job_id: 'j0b00000-5555-6666-7777-888888888888',
  status: 'UPLOADED' as const,
};

function makeFile(): File {
  return new File([new Uint8Array([0x25, 0x50, 0x44, 0x46])], 'c.pdf', {
    type: 'application/pdf',
  });
}

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterAll(() => server.close());

beforeEach(() => {
  __setHttpForTests(testHttp);
});

afterEach(() => {
  server.resetHandlers();
  __setHttpForTests(null);
  __resetForTests();
});

describe('uploadContract — integration с axios-client', () => {
  it('202 → narrowed response (контракт §16.2)', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    server.use(
      mswHttp.post(url(UPLOAD_CONTRACT_ENDPOINT), ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        return HttpResponse.json(OK_RESPONSE, { status: 202 });
      }),
    );

    const result = await uploadContract({ file: makeFile(), title: 'Договор' });

    expect(seenMethod).toBe('POST');
    expect(seenPath).toBe('/api/v1/contracts/upload');
    expect(result.contractId).toBe(OK_RESPONSE.contract_id);
    expect(result.versionId).toBe(OK_RESPONSE.version_id);
    expect(result.status).toBe('UPLOADED');
  });

  it.each([
    ['FILE_TOO_LARGE', 413, 'Файл больше 20 МБ'],
    ['UNSUPPORTED_FORMAT', 415, 'Поддерживается только PDF'],
    ['INVALID_FILE', 400, 'Файл повреждён'],
    ['INTERNAL_ERROR', 500, 'Ошибка на сервере'],
  ])('%s (HTTP %d) → OrchestratorError с error_code и status', async (code, status, msg) => {
    server.use(
      mswHttp.post(url(UPLOAD_CONTRACT_ENDPOINT), () =>
        HttpResponse.json({ error_code: code, message: msg }, { status }),
      ),
    );

    try {
      await uploadContract({ file: makeFile(), title: 't' });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe(code);
      expect(err.status).toBe(status);
      expect(err.message).toBe(msg);
    }
  });

  it('VALIDATION_ERROR с details.fields — fields сохраняются в err.details', async () => {
    server.use(
      mswHttp.post(url(UPLOAD_CONTRACT_ENDPOINT), () =>
        HttpResponse.json(
          {
            error_code: 'VALIDATION_ERROR',
            message: 'Проверьте введённые данные',
            details: {
              fields: [{ field: 'title', code: 'REQUIRED', message: 'Укажите название' }],
            },
          },
          { status: 400 },
        ),
      ),
    );

    try {
      await uploadContract({ file: makeFile(), title: '' });
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('VALIDATION_ERROR');
      const fields = (err.details as { fields?: unknown } | undefined)?.fields;
      expect(fields).toEqual([
        { field: 'title', code: 'REQUIRED', message: 'Укажите название' },
      ]);
    }
  });
});
