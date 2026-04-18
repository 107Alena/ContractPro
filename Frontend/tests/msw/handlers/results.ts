// Handlers для артефактов анализа: results / risks / summary / recommendations.
// Все — GET, idempotent. Зависимый §7.4 retry 502/503 для GET-идемпотентных.

import { http, HttpResponse } from 'msw';

import * as results from '../fixtures/results';
import { type HandlerBase, joinPath } from './_helpers';

export function createResultsHandlers(base: HandlerBase) {
  return [
    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId/results'),
      () => HttpResponse.json(results.analysisResults, { status: 200 }),
    ),

    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId/risks'),
      () => HttpResponse.json(results.riskList, { status: 200 }),
    ),

    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId/summary'),
      () => HttpResponse.json(results.summary, { status: 200 }),
    ),

    http.get(
      joinPath(base, '/contracts/:contractId/versions/:versionId/recommendations'),
      () => HttpResponse.json(results.recommendationList, { status: 200 }),
    ),
  ];
}
