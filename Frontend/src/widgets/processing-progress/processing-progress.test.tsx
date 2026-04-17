// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ProcessingProgress } from './processing-progress';
import { mapStatusToView, PROCESSING_STEPS, stepStateAt } from './step-model';

afterEach(cleanup);

describe('mapStatusToView', () => {
  it('UPLOADED → index 0, progress tone, percent 0', () => {
    const v = mapStatusToView('UPLOADED');
    expect(v.currentIndex).toBe(0);
    expect(v.tone).toBe('progress');
    expect(v.percent).toBe(0);
    expect(v.terminal).toBe(false);
  });

  it('ANALYZING → index 3, progress tone', () => {
    const v = mapStatusToView('ANALYZING');
    expect(v.currentIndex).toBe(3);
    expect(v.tone).toBe('progress');
    expect(v.percent).toBe(60);
  });

  it('AWAITING_USER_INPUT остаётся на шаге ANALYZING с tone=awaiting', () => {
    const v = mapStatusToView('AWAITING_USER_INPUT');
    expect(v.currentIndex).toBe(3);
    expect(v.tone).toBe('awaiting');
    expect(v.terminal).toBe(false);
  });

  it('READY → terminal, tone=success, percent=100', () => {
    const v = mapStatusToView('READY');
    expect(v.tone).toBe('success');
    expect(v.terminal).toBe(true);
    expect(v.percent).toBe(100);
  });

  it('FAILED без errorAtStep — дефолт на PROCESSING', () => {
    const v = mapStatusToView('FAILED');
    expect(v.tone).toBe('error');
    expect(v.currentIndex).toBe(PROCESSING_STEPS.findIndex((s) => s.key === 'PROCESSING'));
  });

  it('FAILED с errorAtStep=GENERATING_REPORTS — привязан к заданному шагу', () => {
    const v = mapStatusToView('FAILED', 'GENERATING_REPORTS');
    expect(v.currentIndex).toBe(4);
  });

  it('PARTIALLY_FAILED без errorAtStep — дефолт на GENERATING_REPORTS', () => {
    const v = mapStatusToView('PARTIALLY_FAILED');
    expect(v.currentIndex).toBe(4);
    expect(v.tone).toBe('error');
    expect(v.terminal).toBe(true);
  });

  it('REJECTED → currentIndex=null, percent=0, terminal', () => {
    const v = mapStatusToView('REJECTED');
    expect(v.currentIndex).toBeNull();
    expect(v.percent).toBe(0);
    expect(v.terminal).toBe(true);
  });
});

describe('stepStateAt', () => {
  it('REJECTED: все шаги pending (pipeline не стартовал)', () => {
    const view = mapStatusToView('REJECTED');
    for (let i = 0; i < PROCESSING_STEPS.length; i += 1) {
      expect(stepStateAt(i, view)).toBe('pending');
    }
  });

  it('PROCESSING: предыдущие done, текущий current, следующие pending', () => {
    const view = mapStatusToView('PROCESSING');
    expect(stepStateAt(0, view)).toBe('done');
    expect(stepStateAt(1, view)).toBe('done');
    expect(stepStateAt(2, view)).toBe('current');
    expect(stepStateAt(3, view)).toBe('pending');
  });

  it('AWAITING_USER_INPUT: шаг 3 — awaiting', () => {
    const view = mapStatusToView('AWAITING_USER_INPUT');
    expect(stepStateAt(3, view)).toBe('awaiting');
  });

  it('READY: все шаги done', () => {
    const view = mapStatusToView('READY');
    for (let i = 0; i < PROCESSING_STEPS.length; i += 1) {
      expect(stepStateAt(i, view)).toBe('done');
    }
  });

  it('FAILED: предыдущие done, error-шаг — error', () => {
    const view = mapStatusToView('FAILED', 'ANALYZING');
    expect(stepStateAt(2, view)).toBe('done');
    expect(stepStateAt(3, view)).toBe('error');
    expect(stepStateAt(4, view)).toBe('pending');
  });
});

describe('ProcessingProgress — рендер', () => {
  it('показывает user-friendly сообщение для UPLOADED', () => {
    render(<ProcessingProgress status="UPLOADED" />);
    // Лейбл «Договор загружен» встречается дважды: в h3 и в list item. Проверяем оба.
    expect(screen.getAllByText('Договор загружен').length).toBe(2);
    expect(screen.getByText('Шаг 1 из 6')).toBeTruthy();
  });

  it('progressbar имеет корректные aria-атрибуты для ANALYZING', () => {
    const { container } = render(<ProcessingProgress status="ANALYZING" />);
    const bar = container.querySelector('[role="progressbar"]');
    expect(bar).toBeTruthy();
    expect(bar?.getAttribute('aria-valuenow')).toBe('60');
    expect(bar?.getAttribute('aria-valuetext')).toBe('Юридический анализ');
    expect(bar?.getAttribute('aria-busy')).toBe('true');
  });

  it('AWAITING_USER_INPUT: aria-busy=true и рендерит awaitingAction slot только на этом шаге', () => {
    const { container } = render(
      <ProcessingProgress
        status="AWAITING_USER_INPUT"
        awaitingAction={<button type="button">Подтвердить</button>}
      />,
    );
    const bar = container.querySelector('[role="progressbar"]');
    expect(bar?.getAttribute('aria-busy')).toBe('true');
    const btn = screen.getByRole('button', { name: 'Подтвердить' });
    expect(btn).toBeTruthy();
    // CTA должна сидеть внутри awaiting-шага (data-state=awaiting), а не в других step-ах
    const awaitingStep = container.querySelector('li[data-state="awaiting"]');
    expect(awaitingStep?.contains(btn)).toBe(true);
  });

  it('awaitingAction игнорируется в не-awaiting состояниях', () => {
    render(
      <ProcessingProgress
        status="ANALYZING"
        awaitingAction={<button type="button">Не должна показаться</button>}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Не должна показаться' })).toBeNull();
  });

  it('READY: aria-valuenow=100 и нет спиннера в header', () => {
    const { container } = render(<ProcessingProgress status="READY" />);
    const bar = container.querySelector('[role="progressbar"]');
    expect(bar?.getAttribute('aria-valuenow')).toBe('100');
    expect(bar?.getAttribute('aria-busy')).toBeNull();
  });

  it('FAILED: показывает error-тон и errorMessage через aria-live', () => {
    const { container } = render(
      <ProcessingProgress status="FAILED" errorMessage="Не удалось распознать PDF" />,
    );
    const err = screen.getByText('Не удалось распознать PDF');
    expect(err).toBeTruthy();
    expect(err.getAttribute('aria-live')).toBe('polite');
    const errorStep = container.querySelector('li[data-state="error"]');
    expect(errorStep).toBeTruthy();
  });

  it('REJECTED: ранний return без progressbar и без списка шагов', () => {
    const { container } = render(
      <ProcessingProgress status="REJECTED" errorMessage="MIME-тип не поддерживается" />,
    );
    expect(screen.getByText('Файл отклонён')).toBeTruthy();
    expect(screen.getByText('MIME-тип не поддерживается')).toBeTruthy();
    expect(container.querySelector('[role="progressbar"]')).toBeNull();
    expect(container.querySelectorAll('li').length).toBe(0);
  });

  it('aria-current="step" проставлен только на текущий шаг', () => {
    const { container } = render(<ProcessingProgress status="PROCESSING" />);
    const currents = container.querySelectorAll('li[aria-current="step"]');
    expect(currents.length).toBe(1);
  });

  it('awaitingAction callback работает (onClick)', () => {
    const onClick = vi.fn();
    render(
      <ProcessingProgress
        status="AWAITING_USER_INPUT"
        awaitingAction={
          <button type="button" onClick={onClick}>
            Подтвердить тип договора
          </button>
        }
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Подтвердить тип договора' }));
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('PARTIALLY_FAILED: error-шаг на GENERATING_REPORTS по умолчанию, прогресс 80%', () => {
    const { container } = render(<ProcessingProgress status="PARTIALLY_FAILED" />);
    const bar = container.querySelector('[role="progressbar"]');
    expect(bar?.getAttribute('aria-valuenow')).toBe('80');
    const errorStep = container.querySelector('li[data-state="error"]');
    expect(errorStep?.textContent).toContain('Формирование отчётов');
  });

  it('рендерит все 6 шагов из PROCESSING_STEPS в каноническом порядке', () => {
    const { container } = render(<ProcessingProgress status="QUEUED" />);
    const items = container.querySelectorAll('ol > li');
    expect(items.length).toBe(6);
    PROCESSING_STEPS.forEach((step, i) => {
      expect(items[i]?.textContent).toContain(step.label);
    });
  });
});
