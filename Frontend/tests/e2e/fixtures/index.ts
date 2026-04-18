// Barrel. Сценарии импортируют `{ test, expect }` ИЗ ЭТОГО ФАЙЛА —
// это гарантирует, что a11y-фикстура подключена, и кастомные helper'ы
// (auth-state) доступны одной import-строкой.

export { expect, test } from './a11y';
export { DEFAULT_MSW_REFRESH_TOKEN, seedAuthenticatedSession } from './auth-state';
