// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { FAQ_ITEMS, FEATURES, HERO_CONTENT, PRICING_PLANS } from './content';
import { LandingPage } from './LandingPage';

afterEach(() => {
  cleanup();
});

function renderLanding() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <LandingPage />
    </MemoryRouter>,
  );
}

describe('LandingPage', () => {
  it('рендерит все 4 секции по aria-labelledby', () => {
    renderLanding();
    const main = screen.getByTestId('page-landing');
    expect(within(main).getByRole('heading', { level: 1, name: HERO_CONTENT.title })).toBeDefined();
    expect(
      within(main).getByRole('heading', {
        level: 2,
        name: /Что умеет ContractPro/i,
      }),
    ).toBeDefined();
    expect(
      within(main).getByRole('heading', {
        level: 2,
        name: /Выберите подходящий план/i,
      }),
    ).toBeDefined();
    expect(
      within(main).getByRole('heading', {
        level: 2,
        name: /Частые вопросы/i,
      }),
    ).toBeDefined();
  });

  it('CTA-кнопка героя ведёт на /login (единственный primary)', () => {
    const { container } = renderLanding();
    // primaryCta.label ("Попробовать бесплатно") совпадает с CTA pro/plus-тарифов —
    // скоупим запрос внутри секции #hero, чтобы получить именно героевую кнопку.
    const hero = container.querySelector('#hero');
    expect(hero).not.toBeNull();
    const primary = within(hero as HTMLElement).getByRole('link', {
      name: HERO_CONTENT.primaryCta.label,
    });
    expect(primary.getAttribute('href')).toBe('/login');
    // секондари-CTA «Запросить демо» убран
    expect(within(hero as HTMLElement).queryByRole('link', { name: 'Запросить демо' })).toBeNull();
  });

  it('рендерит все карточки features', () => {
    renderLanding();
    for (const feature of FEATURES) {
      expect(screen.getByRole('heading', { level: 3, name: feature.title })).toBeDefined();
    }
  });

  it('рендерит все тарифы с CTA-ссылкой внутри секции #pricing', () => {
    const { container } = renderLanding();
    const pricing = container.querySelector('#pricing');
    expect(pricing).not.toBeNull();
    for (const plan of PRICING_PLANS) {
      const heading = within(pricing as HTMLElement).getByRole('heading', {
        level: 3,
        name: plan.name,
      });
      // CTA ищем внутри карточки конкретного плана (pro и plus делят текст CTA),
      // скоупим по ближайшему <li> карточки.
      const card = heading.closest('li');
      expect(card).not.toBeNull();
      const cta = within(card as HTMLElement).getByRole('link', { name: plan.cta.label });
      expect(cta.getAttribute('href')).toBe(plan.cta.to);
    }
  });

  it('рендерит FAQ Accordion с триггерами по вопросам', () => {
    renderLanding();
    for (const item of FAQ_ITEMS) {
      expect(screen.getByRole('button', { name: item.question })).toBeDefined();
    }
  });

  it('секции имеют стабильные id-якоря для deep-link', () => {
    const { container } = renderLanding();
    expect(container.querySelector('#hero')).not.toBeNull();
    expect(container.querySelector('#features')).not.toBeNull();
    expect(container.querySelector('#pricing')).not.toBeNull();
    expect(container.querySelector('#faq')).not.toBeNull();
  });
});
