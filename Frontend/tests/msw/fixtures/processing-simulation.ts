// E2E/dev-only симуляция жизненного цикла обработки договора (§17.4).
//
// Реального backend в `npm run dev:e2e` нет, поэтому статус версии сам по себе
// не меняется. Чтобы экран «Результат проверки договора» показывал прогресс
// анализа и затем результат:
//   - POST /contracts/upload стартует «таймер обработки» (startProcessingSimulation);
//   - GET /contracts/{id} отдаёт статус, вычисленный по прошедшему времени
//     (getSimulatedStatus), проходя канонический pipeline §5.2;
//   - ResultPage поллит GET /contracts/{id} (useContract refetchInterval),
//     поэтому пользователь видит смену шагов и финальный READY.
//
// Только для DEV-сборки: модуль импортируется лишь из tests/msw/* и вместе со
// всем msw-графом вырезается rollup'ом из prod-бандла (см. src/main.tsx).

import type { components } from '@/shared/api/openapi';

type UserProcessingStatus = components['schemas']['UserProcessingStatus'];

interface SimStep {
  /** мс от старта симуляции, начиная с которых активен этот статус. */
  fromMs: number;
  status: UserProcessingStatus;
  message: string;
}

// Канонический порядок шагов (см. processing-progress/step-model.ts
// PROCESSING_STEPS / high-architecture.md §5.2). Длительности подобраны под
// демо: полный прогон ≈ 42 c, шаги заметно сменяют друг друга при поллинге 2 c.
const FIRST_STEP: SimStep = { fromMs: 0, status: 'QUEUED', message: 'В очереди на обработку' };
const TIMELINE: readonly SimStep[] = [
  FIRST_STEP,
  { fromMs: 9_000, status: 'PROCESSING', message: 'Извлечение текста и структуры' },
  { fromMs: 21_000, status: 'ANALYZING', message: 'Юридический анализ' },
  { fromMs: 33_000, status: 'GENERATING_REPORTS', message: 'Формирование отчётов' },
  { fromMs: 42_000, status: 'READY', message: 'Результаты готовы' },
];

const startedAt = new Map<string, number>();

/** Стартует (или перезапускает) симуляцию обработки для договора. */
export function startProcessingSimulation(contractId: string): void {
  startedAt.set(contractId, Date.now());
}

/**
 * Статус текущей версии договора по прошедшему времени симуляции.
 * `null` — симуляция для этого договора не запускалась (handler отдаёт
 * статичную фикстуру без изменений).
 */
export function getSimulatedStatus(
  contractId: string,
): { processing_status: UserProcessingStatus; processing_status_message: string } | null {
  const start = startedAt.get(contractId);
  if (start === undefined) return null;

  const elapsed = Date.now() - start;
  let current: SimStep = FIRST_STEP;
  for (const step of TIMELINE) {
    if (elapsed >= step.fromMs) current = step;
  }

  return {
    processing_status: current.status,
    processing_status_message: current.message,
  };
}
