import { cva } from 'class-variance-authority';
import { type HTMLAttributes, memo, type ReactNode } from 'react';

import { cn } from '@/shared/lib/cn';
import { Spinner } from '@/shared/ui/spinner';

import {
  mapStatusToView,
  PROCESSING_STEPS,
  type ProcessingStepKey,
  type ProcessingView,
  STEPS_TOTAL,
  type StepState,
  stepStateAt,
  type UserProcessingStatus,
} from './step-model';

const rootVariants = cva(['rounded-lg border bg-bg p-5 shadow-sm flex flex-col gap-4', 'text-fg'], {
  variants: {
    tone: {
      progress: 'border-border',
      awaiting: 'border-warning/60',
      error: 'border-danger/60',
      success: 'border-success/60',
    },
  },
  defaultVariants: { tone: 'progress' },
});

const progressFillVariants = cva('h-1.5 rounded-sm transition-[width] duration-300', {
  variants: {
    tone: {
      progress: 'bg-brand-500',
      awaiting: 'bg-warning',
      error: 'bg-danger',
      success: 'bg-success',
    },
  },
  defaultVariants: { tone: 'progress' },
});

export interface ProcessingProgressProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** Статус обработки из API (10 значений из openapi.d.ts UserProcessingStatus). */
  status: UserProcessingStatus;
  /**
   * Для терминальных ошибок (FAILED / PARTIALLY_FAILED) — шаг, на котором ошибка возникла.
   * Если не задан: FAILED → PROCESSING, PARTIALLY_FAILED → GENERATING_REPORTS (backend §5.2).
   * REJECTED игнорирует errorAtStep (pipeline не стартовал).
   */
  errorAtStep?: ProcessingStepKey;
  /** Дополнительное сообщение об ошибке (correlation_id, reason). Отображается под списком шагов. */
  errorMessage?: string;
  /**
   * Slot-компонент для AWAITING_USER_INPUT (FR-2.1.3). Родитель передаёт Button / Link.
   * Рендерится inline-CTA только при `status==='AWAITING_USER_INPUT'`; в других состояниях игнорируется.
   */
  awaitingAction?: ReactNode;
}

export function ProcessingProgress({
  status,
  errorAtStep,
  errorMessage,
  awaitingAction,
  className,
  ...rest
}: ProcessingProgressProps) {
  const view = mapStatusToView(status, errorAtStep);
  const tone = view.tone;
  const awaiting = tone === 'awaiting';

  if (status === 'REJECTED') {
    return (
      <section
        aria-label="Статус обработки договора"
        data-status={status}
        className={cn(rootVariants({ tone: 'error' }), className)}
        {...rest}
      >
        <div className="flex flex-col gap-1">
          <h3 className="text-base font-semibold text-danger">Файл отклонён</h3>
          <p className="text-sm text-fg-muted">
            Документ не принят к обработке (формат, размер или содержимое не прошли валидацию).
          </p>
        </div>
        {errorMessage ? (
          <p className="text-sm text-fg" aria-live="polite">
            {errorMessage}
          </p>
        ) : null}
      </section>
    );
  }

  return (
    <section
      aria-label="Статус обработки договора"
      data-status={status}
      className={cn(rootVariants({ tone }), className)}
      {...rest}
    >
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h3 className="text-base font-semibold">{view.message}</h3>
          <p className="text-sm text-fg-muted">
            Шаг {Math.min((view.currentIndex ?? 0) + 1, STEPS_TOTAL)} из {STEPS_TOTAL}
          </p>
        </div>
        {!view.terminal && !awaiting ? <Spinner size="md" aria-hidden /> : null}
      </div>

      <div
        className="h-1.5 w-full rounded-sm bg-bg-muted"
        role="progressbar"
        aria-label="Прогресс обработки договора"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={view.percent}
        aria-valuetext={view.message}
        aria-busy={awaiting || (!view.terminal && view.tone === 'progress') || undefined}
      >
        <div
          className={progressFillVariants({ tone })}
          style={{ width: `${view.percent}%` }}
          data-testid="processing-progress-fill"
        />
      </div>

      <ol className="flex flex-col gap-2" aria-label="Этапы обработки">
        {PROCESSING_STEPS.map((step, index) => {
          const state = stepStateAt(index, view);
          const isAwaitingHere = awaiting && index === view.currentIndex;
          return (
            <Step
              key={step.key}
              label={step.label}
              state={state}
              action={isAwaitingHere ? awaitingAction : undefined}
            />
          );
        })}
      </ol>

      {view.tone === 'error' && errorMessage ? (
        <p className="text-sm text-danger" aria-live="polite">
          {errorMessage}
        </p>
      ) : null}
    </section>
  );
}

const stepIconVariants = cva(
  ['flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold'],
  {
    variants: {
      state: {
        done: 'border-success bg-success text-white',
        current: 'border-brand-500 bg-brand-50 text-brand-600',
        awaiting: 'border-warning bg-warning/20 text-fg',
        error: 'border-danger bg-danger text-white',
        pending: 'border-border bg-bg text-fg-muted',
      },
    },
  },
);

const stepLabelVariants = cva('text-sm leading-6', {
  variants: {
    state: {
      done: 'text-fg',
      current: 'text-fg font-medium',
      awaiting: 'text-fg font-medium',
      error: 'text-danger font-medium',
      pending: 'text-fg-muted',
    },
  },
});

interface StepProps {
  label: string;
  state: StepState;
  action?: ReactNode;
}

const Step = memo(function Step({ label, state, action }: StepProps) {
  return (
    <li
      className="flex items-start gap-3"
      aria-current={state === 'current' || state === 'awaiting' ? 'step' : undefined}
      data-state={state}
    >
      <span className={stepIconVariants({ state })} aria-hidden>
        {renderIcon(state)}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-2">
        <span className={stepLabelVariants({ state })}>{label}</span>
        {action ? <div className="flex flex-wrap gap-2">{action}</div> : null}
      </div>
    </li>
  );
});

function renderIcon(state: StepState): ReactNode {
  switch (state) {
    case 'done':
      return '✓';
    case 'error':
      return '!';
    case 'awaiting':
      return '…';
    case 'current':
      return <Spinner size="sm" aria-hidden />;
    case 'pending':
    default:
      return '';
  }
}

export type { ProcessingView };
