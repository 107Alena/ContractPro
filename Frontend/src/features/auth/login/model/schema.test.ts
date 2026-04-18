// Тесты loginSchema: клиентская валидация email/password (FE-TASK-029).
import { describe, expect, it } from 'vitest';

import { LOGIN_EMAIL_MAX, LOGIN_PASSWORD_MAX, LOGIN_PASSWORD_MIN, loginSchema } from './schema';

describe('loginSchema', () => {
  it('пропускает валидные email и пароль', () => {
    const result = loginSchema.safeParse({
      email: 'user@example.com',
      password: '12345678',
    });
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.email).toBe('user@example.com');
    }
  });

  it('тримит email (leading/trailing whitespace)', () => {
    const result = loginSchema.safeParse({
      email: '  user@example.com  ',
      password: 'password',
    });
    expect(result.success).toBe(true);
    if (result.success) {
      expect(result.data.email).toBe('user@example.com');
    }
  });

  it('отклоняет пустой email', () => {
    const result = loginSchema.safeParse({ email: '', password: '12345678' });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues.some((i) => i.path[0] === 'email')).toBe(true);
    }
  });

  it('отклоняет email без @', () => {
    const result = loginSchema.safeParse({ email: 'notAnEmail', password: '12345678' });
    expect(result.success).toBe(false);
    if (!result.success) {
      const emailError = result.error.issues.find((i) => i.path[0] === 'email');
      expect(emailError?.message).toMatch(/формат/i);
    }
  });

  it(`отклоняет пароль короче ${LOGIN_PASSWORD_MIN} символов`, () => {
    const result = loginSchema.safeParse({
      email: 'user@example.com',
      password: '1234567',
    });
    expect(result.success).toBe(false);
    if (!result.success) {
      const err = result.error.issues.find((i) => i.path[0] === 'password');
      expect(err?.message).toMatch(/не короче/);
    }
  });

  it(`отклоняет пароль длиннее ${LOGIN_PASSWORD_MAX} символов`, () => {
    const result = loginSchema.safeParse({
      email: 'user@example.com',
      password: 'x'.repeat(LOGIN_PASSWORD_MAX + 1),
    });
    expect(result.success).toBe(false);
  });

  it('отклоняет пустой пароль', () => {
    const result = loginSchema.safeParse({ email: 'user@example.com', password: '' });
    expect(result.success).toBe(false);
  });

  it(`отклоняет email длиннее ${LOGIN_EMAIL_MAX}`, () => {
    const tooLong = `${'a'.repeat(LOGIN_EMAIL_MAX)}@x.ru`;
    const result = loginSchema.safeParse({ email: tooLong, password: '12345678' });
    expect(result.success).toBe(false);
  });
});
