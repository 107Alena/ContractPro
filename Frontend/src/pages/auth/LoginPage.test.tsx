// @vitest-environment jsdom
//
// Тесты LoginPage: sanitizeRedirect (open-redirect защита), рендер и redirect
// для уже-авторизованного пользователя.
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { useSession } from '@/shared/auth';

import { LoginPage, sanitizeRedirect } from './LoginPage';

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('sanitizeRedirect', () => {
  it('возвращает валидный относительный path', () => {
    expect(sanitizeRedirect('/contracts/123')).toBe('/contracts/123');
  });

  it('возвращает fallback для null / пустой строки', () => {
    expect(sanitizeRedirect(null)).toBe('/dashboard');
    expect(sanitizeRedirect('')).toBe('/dashboard');
  });

  it('блокирует absolute URL', () => {
    expect(sanitizeRedirect('https://evil.com/phish')).toBe('/dashboard');
    expect(sanitizeRedirect('http://evil.com')).toBe('/dashboard');
  });

  it('блокирует protocol-relative URL', () => {
    expect(sanitizeRedirect('//evil.com/phish')).toBe('/dashboard');
  });

  it('блокирует backslash-trick', () => {
    expect(sanitizeRedirect('/\\evil.com')).toBe('/dashboard');
  });

  it('блокирует сам /login (anti-loop)', () => {
    expect(sanitizeRedirect('/login')).toBe('/dashboard');
    expect(sanitizeRedirect('/login?x=1')).toBe('/dashboard');
  });

  it('использует явный fallback когда передан', () => {
    expect(sanitizeRedirect(null, '/reports')).toBe('/reports');
    expect(sanitizeRedirect('https://x', '/reports')).toBe('/reports');
  });
});

describe('LoginPage rendering', () => {
  it('рендерит форму для неавторизованного пользователя', () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/dashboard" element={<div data-testid="dashboard-route" />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId('page-login')).toBeDefined();
    expect(screen.getByLabelText(/email/i)).toBeDefined();
    expect(screen.getByLabelText(/пароль/i)).toBeDefined();
    expect(screen.getByRole('button', { name: /войти/i })).toBeDefined();
  });

  it('рендерит заголовок «Вход в ContractPro»', () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <LoginPage />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { level: 1 }).textContent).toMatch(/Вход/i);
  });

  it('редиректит авторизованного пользователя на /dashboard', () => {
    useSession.getState().setAccess('token-abc', 900);

    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/dashboard" element={<div data-testid="dashboard-route" />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId('dashboard-route')).toBeDefined();
    expect(screen.queryByTestId('page-login')).toBeNull();
  });

  it('редиректит авторизованного пользователя на ?redirect=...', () => {
    useSession.getState().setAccess('token-abc', 900);

    render(
      <MemoryRouter initialEntries={['/login?redirect=/reports']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/reports" element={<div data-testid="reports-route" />} />
          <Route path="/dashboard" element={<div data-testid="dashboard-route" />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId('reports-route')).toBeDefined();
  });

  it('игнорирует absolute redirect и идёт на /dashboard', () => {
    useSession.getState().setAccess('token-abc', 900);

    render(
      <MemoryRouter initialEntries={['/login?redirect=https://evil.com']}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/dashboard" element={<div data-testid="dashboard-route" />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByTestId('dashboard-route')).toBeDefined();
  });
});
