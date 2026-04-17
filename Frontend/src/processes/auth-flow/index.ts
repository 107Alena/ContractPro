// Public API auth-flow-процесса (§1.3 public API через barrel).
// softLogout — не экспортируется: используется только изнутри doRefresh-flow.
// Внешние потребители вызывают logout() для happy-path разлогина.
export { doRefresh, login, type LoginCredentials, logout } from './actions';
export { initAuthFlow, setNavigator, teardownAuthFlow } from './setup';
