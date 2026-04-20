// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ExpiredLinkBanner } from './ExpiredLinkBanner';

afterEach(cleanup);

describe('ExpiredLinkBanner', () => {
  it('visible=false → не рендерится', () => {
    render(<ExpiredLinkBanner visible={false} onDismiss={() => {}} />);
    expect(screen.queryByTestId('expired-link-banner')).toBeNull();
  });

  it('visible=true → рендерится с role=status', () => {
    render(<ExpiredLinkBanner visible onDismiss={() => {}} />);
    const banner = screen.getByTestId('expired-link-banner');
    expect(banner).toBeInTheDocument();
    expect(banner.getAttribute('role')).toBe('status');
    expect(banner.getAttribute('aria-live')).toBe('polite');
  });

  it('Dismiss — клик вызывает onDismiss', () => {
    const onDismiss = vi.fn();
    render(<ExpiredLinkBanner visible onDismiss={onDismiss} />);
    fireEvent.click(screen.getByTestId('expired-link-banner-dismiss'));
    expect(onDismiss).toHaveBeenCalledOnce();
  });

  it('onRequestNew не передан → кнопка «Запросить новую ссылку» не рендерится', () => {
    render(<ExpiredLinkBanner visible onDismiss={() => {}} />);
    expect(screen.queryByTestId('expired-link-banner-retry')).toBeNull();
  });

  it('onRequestNew передан → кнопка рендерится и работает', () => {
    const onRequestNew = vi.fn();
    render(<ExpiredLinkBanner visible onDismiss={() => {}} onRequestNew={onRequestNew} />);
    fireEvent.click(screen.getByTestId('expired-link-banner-retry'));
    expect(onRequestNew).toHaveBeenCalledOnce();
  });
});
