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
  it('рендерит бренд и заголовок предложения (figma-aligned)', () => {
    render(<PromoSidebar />);
    const aside = screen.getByRole('complementary', { name: /ContractPro/i });
    expect(aside).toBeDefined();
    expect(aside.textContent).toMatch(/Проверяйте договоры быстрее и без рисков/i);
  });

  it('содержит 4 trust-карточки (figma node 52:8)', () => {
    render(<PromoSidebar />);
    const aside = screen.getByTestId('promo-sidebar');
    expect(aside.querySelectorAll('li').length).toBe(4);
  });
});
