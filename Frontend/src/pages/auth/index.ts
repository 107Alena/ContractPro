// ВАЖНО: барel содержит только компонент-экспорт. router.tsx читает этот
// barrel как `Record<string, ComponentType>` через lazyComponent (см. FE-TASK-031).
// Вспомогательные функции (`sanitizeRedirect`) остаются внутренней
// деталью страницы; тесты импортируют их напрямую из './LoginPage'.
export { LoginPage } from './LoginPage';
