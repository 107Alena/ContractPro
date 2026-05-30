// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { Card } from './card';

afterEach(cleanup);

describe('Card', () => {
  it('рендерит section с card-рецептом по умолчанию', () => {
    render(<Card aria-label="Тест">контент</Card>);
    const el = screen.getByRole('region', { name: 'Тест' });
    expect(el.tagName).toBe('SECTION');
    expect(el.className).toContain('bg-bg');
    expect(el.className).toContain('shadow-sm');
    expect(el.className).toContain('rounded-[12px]');
  });

  it('поддерживает as="article" для вложенных карточек', () => {
    render(<Card as="article" aria-label="Карточка" />);
    expect(screen.getByRole('article', { name: 'Карточка' }).tagName).toBe('ARTICLE');
  });

  it('radius="md" даёт rounded-md', () => {
    render(<Card radius="md" aria-label="Мелкая" />);
    expect(screen.getByRole('region', { name: 'Мелкая' }).className).toContain('rounded-md');
  });

  it('className мёржится без конфликта радиусов (twMerge)', () => {
    render(
      <Card aria-label="Hero" radius="card" className="rounded-xl p-5">
        x
      </Card>,
    );
    const el = screen.getByRole('region', { name: 'Hero' });
    expect(el.className).toContain('rounded-xl');
    expect(el.className).not.toContain('rounded-[12px]');
    expect(el.className).toContain('p-5');
  });
});
