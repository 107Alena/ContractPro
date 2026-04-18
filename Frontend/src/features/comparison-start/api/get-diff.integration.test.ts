// @vitest-environment node
// Integration-тест: реальный axios-client + MSW. Проверяет endpoint,
// 200 narrow и нормализацию 404 DIFF_NOT_FOUND.
import { http as mswHttp, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { __resetForTests, createHttpClient } from '@/shared/api/client';
import { OrchestratorError } from '@/shared/api/errors';

import { server } from '../../../../tests/msw/server';
import { getDiff, getDiffEndpoint } from './get-diff';
import { __setHttpForTests } from './http';

const BASE = 'http://orch.test/api/v1';
const url = (path: string): string => `${BASE}${path}`;

const testHttp = createHttpClient(BASE);
testHttp.defaults.adapter = 'fetch';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const BASE_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const TARGET_ID = 'ta39e700-aaaa-bbbb-cccc-222222222222';

const OK_RESPONSE = {
  base_version_id: BASE_ID,
  target_version_id: TARGET_ID,
  text_diff_count: 2,
  structural_diff_count: 1,
  text_diffs: [
    { type: 'added' as const, path: 'p.1', old_text: null, new_text: 'X' },
    { type: 'modified' as const, path: 'p.2', old_text: 'A', new_text: 'B' },
  ],
  structural_diffs: [
    { type: 'added' as const, node_id: 'n1', old_value: null, new_value: { k: 1 } },
  ],
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

describe('getDiff — integration с axios-client', () => {
  it('200 → narrowed response; path /contracts/{id}/versions/{base}/diff/{target}', async () => {
    let seenPath: string | undefined;
    let seenMethod: string | undefined;
    server.use(
      mswHttp.get(url(getDiffEndpoint(CONTRACT_ID, BASE_ID, TARGET_ID)), ({ request }) => {
        seenPath = new URL(request.url).pathname;
        seenMethod = request.method;
        return HttpResponse.json(OK_RESPONSE, { status: 200 });
      }),
    );

    const result = await getDiff(INPUT);

    expect(seenMethod).toBe('GET');
    expect(seenPath).toBe(
      `/api/v1/contracts/${CONTRACT_ID}/versions/${BASE_ID}/diff/${TARGET_ID}`,
    );
    expect(result.textDiffCount).toBe(2);
    expect(result.structuralDiffCount).toBe(1);
    expect(result.textDiffs).toHaveLength(2);
    expect(result.structuralDiffs).toHaveLength(1);
  });

  it('404 DIFF_NOT_FOUND → OrchestratorError с status=404', async () => {
    server.use(
      mswHttp.get(url(getDiffEndpoint(CONTRACT_ID, BASE_ID, TARGET_ID)), () =>
        HttpResponse.json(
          { error_code: 'DIFF_NOT_FOUND', message: 'Сравнение ещё не готово' },
          { status: 404 },
        ),
      ),
    );

    try {
      await getDiff(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      const err = e as OrchestratorError;
      expect(err.error_code).toBe('DIFF_NOT_FOUND');
      expect(err.status).toBe(404);
    }
  });

  it.each([
    ['VERSION_NOT_FOUND', 404],
    ['PERMISSION_DENIED', 403],
    ['INTERNAL_ERROR', 500],
  ])('%s (HTTP %d) → OrchestratorError', async (code, status) => {
    server.use(
      mswHttp.get(url(getDiffEndpoint(CONTRACT_ID, BASE_ID, TARGET_ID)), () =>
        HttpResponse.json({ error_code: code, message: 'm' }, { status }),
      ),
    );

    try {
      await getDiff(INPUT);
      expect.fail('should throw');
    } catch (e) {
      expect(e).toBeInstanceOf(OrchestratorError);
      expect((e as OrchestratorError).error_code).toBe(code);
      expect((e as OrchestratorError).status).toBe(status);
    }
  });
});
