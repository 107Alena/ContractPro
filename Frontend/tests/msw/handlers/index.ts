// Агрегатор всех handlers. Фабрика `createHandlers(baseURL)` позволяет
// один и тот же набор переиспользовать в node-server (baseURL =
// 'http://localhost/api/v1') и browser-worker (baseURL = '/api/v1').

import type { RequestHandler } from 'msw';

import type { HandlerBase } from './_helpers';
import { createAdminHandlers } from './admin';
import { createAuthHandlers } from './auth';
import { createComparisonHandlers } from './comparison';
import { createContractsHandlers } from './contracts';
import { createExportHandlers } from './export';
import { createFeedbackHandlers } from './feedback';
import { createResultsHandlers } from './results';
import { createSseHandlers } from './sse';
import { createUsersHandlers } from './users';
import { createVersionsHandlers } from './versions';

export function createHandlers(base: HandlerBase): RequestHandler[] {
  return [
    ...createAuthHandlers(base),
    ...createUsersHandlers(base),
    ...createContractsHandlers(base),
    ...createVersionsHandlers(base),
    ...createResultsHandlers(base),
    ...createComparisonHandlers(base),
    ...createExportHandlers(base),
    ...createFeedbackHandlers(base),
    ...createAdminHandlers(base),
    ...createSseHandlers(base),
  ];
}

export {
  createAdminHandlers,
  createAuthHandlers,
  createComparisonHandlers,
  createContractsHandlers,
  createExportHandlers,
  createFeedbackHandlers,
  createResultsHandlers,
  createSseHandlers,
  createUsersHandlers,
  createVersionsHandlers,
};
