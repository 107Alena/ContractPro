// @vitest-environment jsdom
//
// Тесты LoginForm (FE-TASK-029): клиентская валидация, успешная отправка,
// VALIDATION_ERROR с inline-field-маппингом, form-level ошибки (401) с
// clear-password, REQUEST_ABORTED — тихое игнорирование.
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError } from '@/shared/api';

import type { LoginFormValues } from '../model/schema';
import { LoginForm } from './LoginForm';

afterEach(() => {
  cleanup();
});

async function fillAndSubmit(
  email: string,
  password: string,
): Promise<void> {
  const emailInput = screen.getByLabelText(/email/i);
  const passwordInput = screen.getByLabelText(/пароль/i);
  fireEvent.change(emailInput, { target: { value: email } });
  fireEvent.change(passwordInput, { target: { value: password } });
  fireEvent.submit(emailInput.closest('form') as HTMLFormElement);
}

describe('LoginForm — client-side validation', () => {
  it('показывает inline-ошибки при невалидном email и коротком пароле', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<LoginForm onSubmit={onSubmit} />);

    await fillAndSubmit('not-an-email', '123');

    await waitFor(() => {
      expect(screen.getByText(/формат/i)).toBeDefined();
    });
    expect(screen.getByText(/не короче/i)).toBeDefined();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('не отправляет пустую форму', async () => {
    const onSubmit = vi.fn();
    render(<LoginForm onSubmit={onSubmit} />);

    // триггерим submit без заполнения
    const form = screen.getByTestId('login-form') as HTMLFormElement;
    await act(async () => {
      fireEvent.submit(form);
    });

    await waitFor(() => {
      // role=alert теперь только на form-level banner. Inline-хинты —
      // через aria-invalid+aria-describedby. Ищем по тексту сообщений схемы.
      expect(screen.getByText(/введите email/i)).toBeDefined();
    });
    expect(screen.getByText(/введите пароль/i)).toBeDefined();
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('LoginForm — happy path', () => {
  it('вызывает onSubmit с тримнутым email и паролем', async () => {
    const onSubmit = vi.fn<[LoginFormValues], Promise<void>>().mockResolvedValue(undefined);
    render(<LoginForm onSubmit={onSubmit} />);

    await fillAndSubmit('  user@example.com  ', '12345678');

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledTimes(1);
    });
    expect(onSubmit).toHaveBeenCalledWith({
      email: 'user@example.com',
      password: '12345678',
    });
  });
});

describe('LoginForm — server errors', () => {
  it('VALIDATION_ERROR с polem password → inline-ошибка', async () => {
    const serverErr = new OrchestratorError({
      error_code: 'VALIDATION_ERROR',
      message: 'Проверьте данные',
      status: 400,
      details: {
        fields: [
          { field: 'password', code: 'TOO_SHORT', message: 'Пароль слишком короткий' },
        ],
      } as unknown as OrchestratorError['details'],
    });
    const onSubmit = vi.fn().mockRejectedValue(serverErr);
    render(<LoginForm onSubmit={onSubmit} />);

    await fillAndSubmit('user@example.com', 'validpass');

    await waitFor(() => {
      expect(screen.getByText(/Пароль слишком короткий/)).toBeDefined();
    });
    // Form-level баннер не должен появляться — всё сматчилось.
    expect(screen.queryByTestId('login-form-error')).toBeNull();
  });

  it('401 AUTH_TOKEN_INVALID → form-level banner и очистка пароля', async () => {
    const serverErr = new OrchestratorError({
      error_code: 'AUTH_TOKEN_INVALID',
      message: 'Неверный email или пароль',
      status: 401,
    });
    const onSubmit = vi.fn().mockRejectedValue(serverErr);
    render(<LoginForm onSubmit={onSubmit} />);

    await fillAndSubmit('user@example.com', 'validpass');

    await waitFor(() => {
      expect(screen.getByTestId('login-form-error').textContent).toMatch(
        /Неверный email или пароль/,
      );
    });
    // Пароль очищен, email сохранён.
    const pwd = screen.getByLabelText(/пароль/i) as HTMLInputElement;
    expect(pwd.value).toBe('');
    const email = screen.getByLabelText(/email/i) as HTMLInputElement;
    expect(email.value).toBe('user@example.com');
  });

  it('REQUEST_ABORTED тихо игнорируется (не показывает form-error)', async () => {
    const serverErr = new OrchestratorError({
      error_code: 'REQUEST_ABORTED',
      message: 'Отменено',
    });
    const onSubmit = vi.fn().mockRejectedValue(serverErr);
    render(<LoginForm onSubmit={onSubmit} />);

    await fillAndSubmit('user@example.com', 'validpass');

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalled();
    });
    expect(screen.queryByTestId('login-form-error')).toBeNull();
  });

  it('кнопка Войти показывает loading во время submit', async () => {
    let resolveFn: (() => void) | undefined;
    const onSubmit = vi.fn().mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          resolveFn = resolve;
        }),
    );
    render(<LoginForm onSubmit={onSubmit} />);

    await fillAndSubmit('user@example.com', 'validpass');

    await waitFor(() => {
      expect(
        (screen.getByRole('button', { name: /войти/i }) as HTMLButtonElement).disabled,
      ).toBe(true);
    });
    // Разрешаем promise, чтобы не утекать state между тестами.
    resolveFn?.();
  });
});

describe('LoginForm — a11y', () => {
  it('имеет aria-invalid на невалидных полях', async () => {
    render(<LoginForm onSubmit={vi.fn()} />);
    await fillAndSubmit('bad', '1');
    await waitFor(() => {
      expect(screen.getByLabelText(/email/i).getAttribute('aria-invalid')).toBe('true');
    });
  });

  it('defaultEmail предзаполняет поле', () => {
    render(<LoginForm onSubmit={vi.fn()} defaultEmail="admin@company.ru" />);
    const email = screen.getByLabelText(/email/i) as HTMLInputElement;
    expect(email.value).toBe('admin@company.ru');
  });
});
