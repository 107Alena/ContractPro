// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { DiffToolbar } from './diff-toolbar';

afterEach(cleanup);

describe('DiffToolbar', () => {
  it('рендерит обе кнопки и счётчики', () => {
    render(
      <DiffToolbar
        mode="side-by-side"
        onModeChange={() => {}}
        totalParagraphs={42}
        totalChanges={7}
      />,
    );
    expect(screen.getByRole('button', { name: 'Бок о бок' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'В одну колонку' })).toBeTruthy();
    expect(screen.getByText('42')).toBeTruthy();
    expect(screen.getByText('7')).toBeTruthy();
    // active кнопка имеет aria-pressed=true
    const sideButton = screen.getByRole('button', { name: 'Бок о бок' });
    expect(sideButton.getAttribute('aria-pressed')).toBe('true');
    const inlineButton = screen.getByRole('button', { name: 'В одну колонку' });
    expect(inlineButton.getAttribute('aria-pressed')).toBe('false');
  });

  it('клик по второй кнопке вызывает onModeChange("inline")', () => {
    const onModeChange = vi.fn();
    render(
      <DiffToolbar
        mode="side-by-side"
        onModeChange={onModeChange}
        totalParagraphs={1}
        totalChanges={0}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'В одну колонку' }));
    expect(onModeChange).toHaveBeenCalledOnce();
    expect(onModeChange).toHaveBeenCalledWith('inline');
  });
});
