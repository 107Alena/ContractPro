import type { components } from '@/shared/api/openapi';

export type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

/**
 * Линейный «pipeline» из 6 шагов — канонический порядок обработки договора.
 * Источник лейблов: ApiBackendOrchestrator/architecture/high-architecture.md §5.2.
 * AWAITING_USER_INPUT не является самостоятельным шагом — это пауза на шаге ANALYZING;
 * обрабатывается в `mapStatusToView` (tone='awaiting' на stepIndex=3).
 */
export const PROCESSING_STEPS = [
  { key: 'UPLOADED', label: 'Договор загружен' },
  { key: 'QUEUED', label: 'В очереди на обработку' },
  { key: 'PROCESSING', label: 'Извлечение текста и структуры' },
  { key: 'ANALYZING', label: 'Юридический анализ' },
  { key: 'GENERATING_REPORTS', label: 'Формирование отчётов' },
  { key: 'READY', label: 'Результаты готовы' },
] as const;

export type ProcessingStepKey = (typeof PROCESSING_STEPS)[number]['key'];

export const STEPS_TOTAL = PROCESSING_STEPS.length;

/** Финальный UI-тон текущего шага для выбора иконки/цвета. */
export type StepTone = 'progress' | 'awaiting' | 'error' | 'success';

/** Статус одного шага в списке (не путать с tone текущего шага). */
export type StepState = 'done' | 'current' | 'pending' | 'error' | 'awaiting';

export interface ProcessingView {
  /** Индекс активного шага в PROCESSING_STEPS (0..5). Для REJECTED — null (pipeline не стартовал). */
  currentIndex: number | null;
  /** Тон активного шага/виджета в целом. */
  tone: StepTone;
  /** Короткое user-friendly сообщение (рус.), соответствующее статусу. */
  message: string;
  /** Терминальный ли статус (READY / FAILED / REJECTED / PARTIALLY_FAILED). */
  terminal: boolean;
  /** Процент прогресса 0..100 (для progressbar). Для awaiting оставляем последний прогресс; для REJECTED — 0. */
  percent: number;
}

/**
 * Маппинг `status` + опциональный `errorAtStep` → UI-представление.
 * Терминальные ошибочные статусы (FAILED / PARTIALLY_FAILED) привязываются к шагу, на котором
 * они возникли, через errorAtStep (если потребитель его не задал — используется дефолт §5.2:
 * FAILED → PROCESSING, PARTIALLY_FAILED → GENERATING_REPORTS).
 * REJECTED особый — pipeline не стартовал (pre-processing валидация), currentIndex=null.
 */
export function mapStatusToView(
  status: UserProcessingStatus,
  errorAtStep?: ProcessingStepKey,
): ProcessingView {
  switch (status) {
    case 'UPLOADED':
      return viewAt(0, 'progress', 'Договор загружен');
    case 'QUEUED':
      return viewAt(1, 'progress', 'В очереди на обработку');
    case 'PROCESSING':
      return viewAt(2, 'progress', 'Извлечение текста и структуры');
    case 'ANALYZING':
      return viewAt(3, 'progress', 'Юридический анализ');
    case 'AWAITING_USER_INPUT':
      return viewAt(3, 'awaiting', 'Требуется подтверждение типа договора');
    case 'GENERATING_REPORTS':
      return viewAt(4, 'progress', 'Формирование отчётов');
    case 'READY':
      return {
        currentIndex: 5,
        tone: 'success',
        message: 'Результаты готовы',
        terminal: true,
        percent: 100,
      };
    case 'PARTIALLY_FAILED': {
      const idx = indexOfStep(errorAtStep ?? 'GENERATING_REPORTS');
      return {
        currentIndex: idx,
        tone: 'error',
        message: 'Частично доступно (есть ошибки)',
        terminal: true,
        percent: percentOf(idx),
      };
    }
    case 'FAILED': {
      const idx = indexOfStep(errorAtStep ?? 'PROCESSING');
      return {
        currentIndex: idx,
        tone: 'error',
        message: 'Ошибка обработки',
        terminal: true,
        percent: percentOf(idx),
      };
    }
    case 'REJECTED':
      return {
        currentIndex: null,
        tone: 'error',
        message: 'Файл отклонён',
        terminal: true,
        percent: 0,
      };
  }
}

/** Статус шага списка при заданном currentIndex/tone. */
export function stepStateAt(stepIndex: number, view: ProcessingView): StepState {
  if (view.currentIndex === null) return 'pending';
  if (stepIndex < view.currentIndex) return 'done';
  if (stepIndex > view.currentIndex) return 'pending';
  // stepIndex === currentIndex
  if (view.tone === 'awaiting') return 'awaiting';
  if (view.tone === 'error') return 'error';
  if (view.tone === 'success') return 'done';
  return 'current';
}

function viewAt(index: number, tone: StepTone, message: string): ProcessingView {
  return { currentIndex: index, tone, message, terminal: false, percent: percentOf(index) };
}

function indexOfStep(key: ProcessingStepKey): number {
  const found = PROCESSING_STEPS.findIndex((s) => s.key === key);
  return found === -1 ? 0 : found;
}

function percentOf(index: number): number {
  if (index <= 0) return 0;
  return Math.round((index / (STEPS_TOTAL - 1)) * 100);
}
