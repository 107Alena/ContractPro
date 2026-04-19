// @vitest-environment jsdom
//
// Тесты UI запускаются в jsdom, где Worker===undefined → useDiffWorker
// синхронно вызывает computeAllDiffs. Это позволяет проверять отрисовку
// без mock-ов Worker.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { DiffParagraph } from '../model/types';
import { DiffViewer } from './diff-viewer';

afterEach(cleanup);

const SAMPLE_PARAGRAPHS: readonly DiffParagraph[] = [
  {
    id: 'p1',
    baseText: 'Стороны заключили договор.',
    targetText: 'Стороны заключили договор поставки.',
    status: 'modified',
  },
  {
    id: 'p2',
    baseText: '',
    targetText: 'Новый пункт 2.5 о порядке расчётов.',
    status: 'added',
  },
  {
    id: 'p3',
    baseText: 'Старый пункт о неустойке.',
    targetText: '',
    status: 'removed',
  },
];

describe('DiffViewer', () => {
  it('показывает loading state при isLoading=true', () => {
    render(<DiffViewer isLoading paragraphs={SAMPLE_PARAGRAPHS} />);
    const root = screen.getByTestId('diff-viewer-root');
    expect(root.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByTestId('diff-viewer-loading')).toBeTruthy();
    expect(screen.getByText('Подсчитываем diff…')).toBeTruthy();
  });

  it('показывает error state и retry-кнопку', () => {
    let retryCalls = 0;
    render(
      <DiffViewer
        paragraphs={SAMPLE_PARAGRAPHS}
        error={new Error('Comparison job failed')}
        onRetry={() => {
          retryCalls += 1;
        }}
      />,
    );
    const errorBlock = screen.getByTestId('diff-viewer-error');
    expect(errorBlock).toBeTruthy();
    expect(screen.getByText('Не удалось посчитать diff')).toBeTruthy();
    expect(screen.getByText('Comparison job failed')).toBeTruthy();
    fireEvent.click(screen.getByTestId('diff-viewer-retry'));
    expect(retryCalls).toBe(1);
  });

  it('показывает empty state, когда paragraphs=[] ', () => {
    render(<DiffViewer paragraphs={[]} />);
    expect(screen.getByTestId('diff-viewer-empty')).toBeTruthy();
    expect(screen.getByText('Изменений нет')).toBeTruthy();
  });

  it('показывает empty state, когда paragraphs=undefined', () => {
    render(<DiffViewer />);
    expect(screen.getByTestId('diff-viewer-empty')).toBeTruthy();
  });

  it('side-by-side: каждая diff-row содержит две дочерние колонки (grid-cols-2)', () => {
    render(<DiffViewer paragraphs={SAMPLE_PARAGRAPHS} />);
    const rows = screen.getAllByTestId('diff-row');
    expect(rows.length).toBeGreaterThan(0);
    // По дефолту mode=side-by-side: grid-cols-2 → две дочерние div-колонки
    const firstRow = rows[0];
    expect(firstRow).toBeDefined();
    expect(firstRow?.className).toContain('grid-cols-2');
    expect(firstRow?.children.length).toBe(2);
  });

  it('inline (после клика на toggle): одна колонка + маркер +/-/~', () => {
    render(<DiffViewer paragraphs={SAMPLE_PARAGRAPHS} />);
    fireEvent.click(screen.getByRole('button', { name: 'В одну колонку' }));
    const rows = screen.getAllByTestId('diff-row');
    const firstRow = rows[0];
    expect(firstRow).toBeDefined();
    expect(firstRow?.className).not.toContain('grid-cols-2');
    // в inline один маркер + один контент → 2 child
    expect(firstRow?.children.length).toBe(2);
  });

  it('uncontrolled toggle между режимами обновляет aria-pressed', () => {
    render(<DiffViewer paragraphs={SAMPLE_PARAGRAPHS} />);
    const sideBtn = screen.getByRole('button', { name: 'Бок о бок' });
    const inlineBtn = screen.getByRole('button', { name: 'В одну колонку' });
    expect(sideBtn.getAttribute('aria-pressed')).toBe('true');
    expect(inlineBtn.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(inlineBtn);
    expect(sideBtn.getAttribute('aria-pressed')).toBe('false');
    expect(inlineBtn.getAttribute('aria-pressed')).toBe('true');
  });

  it('считает diff синхронно в jsdom (Worker===undefined fallback)', () => {
    // если бы fallback не сработал, ничего бы не отрисовалось — рендер бы
    // застрял на isComputing=true (loading state).
    render(<DiffViewer paragraphs={SAMPLE_PARAGRAPHS} />);
    expect(screen.queryByTestId('diff-viewer-loading')).toBeNull();
    expect(screen.getByTestId('diff-viewer-root')).toBeTruthy();
    expect(screen.getAllByTestId('diff-row').length).toBe(SAMPLE_PARAGRAPHS.length);
  });

  it('toolbar показывает корректные счётчики (всего параграфов и изменений)', () => {
    render(
      <DiffViewer
        paragraphs={[
          ...SAMPLE_PARAGRAPHS,
          {
            id: 'p4',
            baseText: 'Без изменений.',
            targetText: 'Без изменений.',
            status: 'unchanged',
          },
        ]}
      />,
    );
    // 4 параграфа всего, 3 изменения (unchanged исключён).
    expect(screen.getByText('4')).toBeTruthy();
    expect(screen.getByText('3')).toBeTruthy();
  });

  it('controlled mode: onModeChange вызывается, но внутреннее состояние не меняется', () => {
    const calls: string[] = [];
    const { rerender } = render(
      <DiffViewer
        paragraphs={SAMPLE_PARAGRAPHS}
        mode="side-by-side"
        onModeChange={(m) => calls.push(m)}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'В одну колонку' }));
    expect(calls).toEqual(['inline']);
    // Без обновления props mode, стиль остаётся side-by-side.
    const rows = screen.getAllByTestId('diff-row');
    expect(rows[0]?.className).toContain('grid-cols-2');
    // После rerender с новым mode — переключается.
    rerender(
      <DiffViewer
        paragraphs={SAMPLE_PARAGRAPHS}
        mode="inline"
        onModeChange={(m) => calls.push(m)}
      />,
    );
    const rowsAfter = screen.getAllByTestId('diff-row');
    expect(rowsAfter[0]?.className).not.toContain('grid-cols-2');
  });
});
