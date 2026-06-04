// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import type { UserProfile } from '@/entities/user';

import { WelcomeBlock } from './WelcomeBlock';

const user: UserProfile = {
  user_id: 'u1',
  email: 'maria@company.ru',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: 'o1',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

function renderWith(u?: UserProfile) {
  return render(
    <MemoryRouter>
      <WelcomeBlock user={u} />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe('WelcomeBlock', () => {
  it('приветствует по имени (первое слово из name)', () => {
    renderWith(user);
    expect(
      screen.getByRole('heading', { level: 1, name: 'Добро пожаловать, Мария' }),
    ).toBeDefined();
  });

  it('без пользователя — нейтральное приветствие', () => {
    renderWith(undefined);
    expect(screen.getByRole('heading', { level: 1, name: 'Добро пожаловать' })).toBeDefined();
  });

  it('единственный CTA «Новая проверка договора» ведёт на /contracts/new', () => {
    renderWith(user);
    const links = screen.getAllByRole('link');
    expect(links).toHaveLength(1);
    expect(links[0]?.textContent).toBe('Новая проверка договора');
    expect(links[0]?.getAttribute('href')).toBe('/contracts/new');
  });

  it('микрокопия отражает реальные ограничения (PDF, 20 МБ)', () => {
    renderWith(user);
    expect(screen.getByText(/PDF · до 20 МБ/)).toBeDefined();
  });
});
