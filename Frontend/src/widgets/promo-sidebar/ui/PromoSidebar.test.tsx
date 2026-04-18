// @vitest-environment jsdom
//
// Smoke-тесты PromoSidebar: рендер ключевых элементов и корректный ARIA-label.
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { PromoSidebar } from './PromoSidebar';

afterEach(() => {
  cleanup();
});

describe('PromoSidebar', () => {
  it('рендерит бренд и заголовок предложения', () => {
    render(<PromoSidebar />);
    const aside = screen.getByRole('complementary', { name: /ContractPro/i });
    expect(aside).toBeDefined();
    expect(aside.textContent).toMatch(/ИИ-проверка договоров/i);
  });

  it('содержит три highlight-пункта', () => {
    render(<PromoSidebar />);
    const aside = screen.getByTestId('promo-sidebar');
    expect(aside.querySelectorAll('li').length).toBe(3);
  });
});
