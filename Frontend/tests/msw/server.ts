// Node-server для vitest (environment=jsdom или node). Абсолютный baseURL
// 'http://localhost/api/v1' анкорит handlers к localhost-origin, чтобы
// handler-list global-server'а не пересекался с legacy-тестами, которые
// используют иной origin (например, http://orch.test/api/v1).
//
// Подключён через Frontend/src/test-setup.ts — lifecycle (listen/reset/close)
// описан там. В тестах импортируйте именно этот `server`, а не создавайте
// новый `setupServer(...)` — это приведёт к конфликту interceptor-listeners
// (предупреждение code-architect в FE-TASK-054).

import { setupServer } from 'msw/node';

import { createHandlers } from './handlers';

export const SERVER_BASE_URL = 'http://localhost/api/v1';

export const server = setupServer(...createHandlers(SERVER_BASE_URL));
