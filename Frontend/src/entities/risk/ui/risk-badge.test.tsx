import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { TooltipProvider } from '@/shared/ui';

import { RISK_LEVELS } from '../model';
import { RiskBadge } from './risk-badge';

afterEach(() => {
  cleanup();
});

describe('RiskBadge', () => {
  it.each(RISK_LEVELS)('рендерит лейбл для уровня "%s"', (level) => {
    render(<RiskBadge level={level} />);
    const badge = screen.getByTestId('risk-badge');
    expect(badge.getAttribute('data-level')).toBe(level);
    expect(badge.textContent).toMatch(/риск/i);
  });

  it('high использует danger-tone (bg-danger через color-mix)', () => {
    render(<RiskBadge level="high" />);
    const badge = screen.getByTestId('risk-badge');
    expect(badge.className).toContain('text-danger');
  });

  it('medium использует warning-tone', () => {
    render(<RiskBadge level="medium" />);
    const badge = screen.getByTestId('risk-badge');
    expect(badge.className).toContain('text-fg');
  });

  it('low использует success-tone', () => {
    render(<RiskBadge level="low" />);
    const badge = screen.getByTestId('risk-badge');
    expect(badge.className).toContain('text-success');
  });

  it('по умолчанию НЕ оборачивает в tooltip (showTooltip=false)', () => {
    const { container } = render(<RiskBadge level="high" />);
    // SimpleTooltip рендерит Radix Tooltip.Trigger asChild → обёртки вокруг
    // badge не появляется; в дереве только сам span.
    expect(container.children.length).toBe(1);
    expect(container.firstElementChild?.tagName).toBe('SPAN');
  });

  it('showTooltip=true оборачивает badge в Radix Tooltip trigger', () => {
    render(
      <TooltipProvider delayDuration={0}>
        <RiskBadge level="medium" showTooltip />
      </TooltipProvider>,
    );
    const badge = screen.getByTestId('risk-badge');
    // Radix Tooltip.Trigger + asChild добавляют data-state к потомку-триггеру.
    expect(badge.getAttribute('data-state')).toBeDefined();
  });

  it('позволяет переопределить children', () => {
    render(
      <RiskBadge level="high">
        <span>Custom</span>
      </RiskBadge>,
    );
    expect(screen.getByTestId('risk-badge').textContent).toBe('Custom');
  });

  it('пробрасывает дополнительные HTML-атрибуты и aria-label', () => {
    render(<RiskBadge level="low" aria-label="Низкий риск по статье 307" id="r-42" />);
    const badge = screen.getByTestId('risk-badge');
    expect(badge.getAttribute('aria-label')).toBe('Низкий риск по статье 307');
    expect(badge.id).toBe('r-42');
  });
});
