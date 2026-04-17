import './app/styles/index.css';
import './shared/i18n/config';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App';
import { initAuthFlow } from './processes/auth-flow';
import { initSentry } from './shared/observability';

initSentry();
// Должен быть зарегистрирован до createRoot: React Router data-loaders запускаются
// синхронно при mount'е, и первый 401 AUTH_TOKEN_EXPIRED должен попасть в
// shared-promise refresh-flow, а не в NotFound /login-spinner (§5.1, §5.3).
initAuthFlow();

const container = document.getElementById('root');
if (!container) {
  throw new Error('Root container #root not found in index.html');
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
