import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { UserProcessingStatus } from '@/shared/api';

import { StatusBadge } from './status-badge';

afterEach(() => {
  cleanup();
});

const ALL_STATUSES: readonly UserProcessingStatus[] = [
  'UPLOADED',
  'QUEUED',
  'PROCESSING',
  'ANALYZING',
  'AWAITING_USER_INPUT',
  'GENERATING_REPORTS',
  'READY',
  'PARTIALLY_FAILED',
  'FAILED',
  'REJECTED',
];

describe('StatusBadge', () => {
  it.each(ALL_STATUSES)('рендерит локализованный лейбл для "%s"', (status) => {
    render(<StatusBadge status={status} />);
    const badge = screen.getByTestId('status-badge');
    expect(badge.getAttribute('data-status')).toBe(status);
    expect(badge.textContent).toBeTruthy();
    expect(badge.textContent!.length).toBeGreaterThan(0);
  });

  it('READY → success tone (text-success)', () => {
    render(<StatusBadge status="READY" />);
    expect(screen.getByTestId('status-badge').className).toContain('text-success');
  });

  it('FAILED → danger tone', () => {
    render(<StatusBadge status="FAILED" />);
    expect(screen.getByTestId('status-badge').className).toContain('text-danger');
  });

  it('PROCESSING → brand tone', () => {
    render(<StatusBadge status="PROCESSING" />);
    expect(screen.getByTestId('status-badge').className).toContain('text-brand-600');
  });

  it('undefined → neutral tone + «Неизвестно»', () => {
    render(<StatusBadge status={undefined} />);
    const badge = screen.getByTestId('status-badge');
    expect(badge.textContent).toBe('Неизвестно');
    expect(badge.getAttribute('data-status')).toBe('UNKNOWN');
    expect(badge.className).toContain('bg-bg-muted');
  });

  it('null → fallback до «Неизвестно»', () => {
    render(<StatusBadge status={null} />);
    expect(screen.getByTestId('status-badge').textContent).toBe('Неизвестно');
  });

  it('children переопределяет дефолтный label', () => {
    render(<StatusBadge status="READY">Custom</StatusBadge>);
    expect(screen.getByTestId('status-badge').textContent).toBe('Custom');
  });

  it('пробрасывает HTML-атрибуты (aria, id, title)', () => {
    render(
      <StatusBadge
        status="AWAITING_USER_INPUT"
        aria-label="Требует подтверждения типа договора"
        id="status-42"
        title="tooltip"
      />,
    );
    const badge = screen.getByTestId('status-badge');
    expect(badge.getAttribute('aria-label')).toBe('Требует подтверждения типа договора');
    expect(badge.id).toBe('status-42');
    expect(badge.getAttribute('title')).toBe('tooltip');
  });
});
