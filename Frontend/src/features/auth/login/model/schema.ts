// Zod-схема формы логина (FE-TASK-029, §17.4 Auth Page, §5.1 auth-flow).
//
// Клиентская валидация — первая линия защиты (NFR: мгновенная обратная связь
// без round-trip). Backend валидирует дополнительно и может вернуть
// `VALIDATION_ERROR` с `details.fields`, который маппится на форму через
// `applyValidationErrors` (§20.4a). Имена полей (`email`, `password`) совпадают
// с `LoginRequest.email/password` из OpenAPI — это упрощает маппинг без
// rename-таблиц.
//
// Трим email выполняется на уровне схемы (`.trim()`) — избавляет от случайного
// пробела при copy-paste, сохраняя простоту валидации (email сервера
// чувствителен к leading/trailing whitespace).
import { z } from 'zod';

export const LOGIN_EMAIL_MAX = 254;
export const LOGIN_PASSWORD_MIN = 8;
export const LOGIN_PASSWORD_MAX = 128;

export const loginSchema = z.object({
  email: z
    .string({ required_error: 'Введите email' })
    .trim()
    .min(1, 'Введите email')
    .max(LOGIN_EMAIL_MAX, `Email не должен быть длиннее ${LOGIN_EMAIL_MAX} символов`)
    .email('Неверный формат email'),
  password: z
    .string({ required_error: 'Введите пароль' })
    .min(1, 'Введите пароль')
    .min(LOGIN_PASSWORD_MIN, `Пароль должен быть не короче ${LOGIN_PASSWORD_MIN} символов`)
    .max(LOGIN_PASSWORD_MAX, `Пароль не должен быть длиннее ${LOGIN_PASSWORD_MAX} символов`),
});

export type LoginFormValues = z.infer<typeof loginSchema>;
