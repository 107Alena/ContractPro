// Централизованный каталог UX-сообщений по кодам ошибок (§7.3 high-architecture).
//
// Правило: к финальному UI прокидывается `message` из тела ответа (он уже на
// русском — NFR-5.2), ERROR_UX используется как fallback title и как источник
// `action`. Никаких HTML/JSX здесь — только чистые строки, чтобы каталог был
// сериализуем и переиспользуем в e-mail/toast/dialog-баннерах.

import type { ErrorAction, ErrorCode } from './codes';

export interface ErrorUXEntry {
  /** Заголовок UI-уведомления (fallback, если тело ответа не содержит message). */
  title: string;
  /** Подсказка следующего шага (например, «Сократите объём или разделите документ»). */
  hint?: string;
  /** Какую кнопку показать: повтор, логин, ничего. undefined → UI решает сам. */
  action?: ErrorAction;
}

export const ERROR_UX: Record<ErrorCode, ErrorUXEntry> = {
  AUTH_TOKEN_MISSING: { title: 'Требуется вход в систему', action: 'login' },
  AUTH_TOKEN_EXPIRED: { title: 'Сессия истекла. Войдите заново.', action: 'login' },
  AUTH_TOKEN_INVALID: { title: 'Невалидная авторизация. Войдите заново.', action: 'login' },
  PERMISSION_DENIED: { title: 'У вас нет прав на это действие', action: 'none' },
  FILE_TOO_LARGE: {
    title: 'Файл больше 20 МБ',
    hint: 'Сократите объём или разделите документ.',
  },
  UNSUPPORTED_FORMAT: {
    title: 'Поддерживается только PDF',
    hint: 'Сохраните документ в PDF и повторите.',
  },
  INVALID_FILE: { title: 'Файл повреждён или не читается' },
  DOCUMENT_NOT_FOUND: { title: 'Документ не найден' },
  VERSION_NOT_FOUND: { title: 'Версия не найдена' },
  ARTIFACT_NOT_FOUND: { title: 'Результат пока недоступен. Повторите позже.' },
  DIFF_NOT_FOUND: { title: 'Сравнение ещё не готово' },
  DOCUMENT_ARCHIVED: { title: 'Документ в архиве. Действие недоступно.' },
  DOCUMENT_DELETED: { title: 'Документ удалён' },
  VERSION_STILL_PROCESSING: {
    title: 'Версия ещё обрабатывается',
    hint: 'Дождитесь завершения.',
  },
  RESULTS_NOT_READY: { title: 'Результаты ещё не готовы' },
  RATE_LIMIT_EXCEEDED: {
    title: 'Слишком много запросов',
    hint: 'Повторите через несколько секунд.',
    action: 'retry',
  },
  STORAGE_UNAVAILABLE: { title: 'Хранилище временно недоступно', action: 'retry' },
  DM_UNAVAILABLE: { title: 'Сервис временно недоступен', action: 'retry' },
  OPM_UNAVAILABLE: { title: 'Сервис политик временно недоступен', action: 'retry' },
  BROKER_UNAVAILABLE: { title: 'Обработка временно недоступна', action: 'retry' },
  VALIDATION_ERROR: { title: 'Проверьте введённые данные' },
  INTERNAL_ERROR: { title: 'Произошла ошибка. Мы уже знаем.', action: 'retry' },

  // Клиентские sentinel-коды (§7.2). Возникают ДО ответа сервера: сеть,
  // таймаут, отмена запроса, неклассифицируемое. UX-action 'retry' разумен
  // для network/timeout; 'none' — для aborted (пользователь сам отменил).
  NETWORK_ERROR: {
    title: 'Нет соединения с сервером',
    hint: 'Проверьте подключение к интернету и повторите.',
    action: 'retry',
  },
  TIMEOUT: {
    title: 'Превышено время ожидания',
    hint: 'Сервер не ответил вовремя. Повторите запрос.',
    action: 'retry',
  },
  REQUEST_ABORTED: { title: 'Запрос отменён', action: 'none' },
  UNKNOWN_ERROR: { title: 'Произошла ошибка. Мы уже знаем.', action: 'retry' },
};
