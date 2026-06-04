// Handlers для артефактов анализа: results / risks / summary / recommendations.
// Все — GET, idempotent. Зависимый §7.4 retry 502/503 для GET-идемпотентных.

import { http, HttpResponse } from 'msw';

import * as results from '../fixtures/results';
import { type HandlerBase, joinPath } from './_helpers';

export function createResultsHandlers(base: HandlerBase) {
  return [
    http.get(joinPath(base, '/contracts/:contractId/versions/:versionId/results'), () =>
      HttpResponse.json(results.analysisResults, { status: 200 }),
    ),

    // Per-version риски: delta v1/v2 имеют разные наборы (наглядная риск-дельта
    // на сравнении), остальные версии — дефолтный riskList.
    http.get(joinPath(base, '/contracts/:contractId/versions/:versionId/risks'), ({ params }) =>
      HttpResponse.json(results.risksByVersionId[params.versionId as string] ?? results.riskList, {
        status: 200,
      }),
    ),

    http.get(joinPath(base, '/contracts/:contractId/versions/:versionId/summary'), () =>
      HttpResponse.json(results.summary, { status: 200 }),
    ),

    http.get(joinPath(base, '/contracts/:contractId/versions/:versionId/recommendations'), () =>
      HttpResponse.json(results.recommendationList, { status: 200 }),
    ),
  ];
}
