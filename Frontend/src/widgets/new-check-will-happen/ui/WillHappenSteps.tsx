// WillHappenSteps — информационный блок «Что произойдёт после загрузки»
// на NewCheckPage (§17.4, FE-TASK-043). Presentational-only: статичный
// список из 4 шагов обработки договора (upload → ОЦР → анализ → результат).
// Источник шагов: §5.2 (pipeline) ApiBackendOrchestrator/architecture.
import { type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

export interface WillHappenStepsProps extends HTMLAttributes<HTMLElement> {
  className?: string;
}

const STEPS: Array<{ title: string; description: string }> = [
  {
    title: 'Загрузка и распознавание',
    description: 'Проверим формат, извлечём текст. Сканы обрабатываем через OCR.',
  },
  {
    title: 'Классификация и структура',
    description: 'Определим тип договора и разобьём его на разделы и пункты.',
  },
  {
    title: 'Юридический анализ',
    description: 'Найдём риски, отклонения и отсутствующие обязательные условия.',
  },
  {
    title: 'Готовые отчёты',
    description: 'Резюме для бизнеса, детальный отчёт и рекомендации по формулировкам.',
  },
];

export function WillHappenSteps({ className, ...rest }: WillHappenStepsProps): JSX.Element {
  return (
    <section
      aria-label="Что произойдёт"
      className={cn(
        'flex flex-col gap-4 rounded-md border border-border bg-bg-muted p-5',
        className,
      )}
      {...rest}
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Что произойдёт
        </h2>
        <p className="mt-1 text-base font-semibold text-fg">
          Обычно занимает 1–2 минуты
        </p>
      </header>

      <ol className="flex flex-col gap-3">
        {STEPS.map((step, index) => (
          <li key={step.title} className="flex items-start gap-3">
            <span
              aria-hidden
              className="mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-brand-500 bg-brand-50 text-xs font-semibold text-brand-600"
            >
              {index + 1}
            </span>
            <div className="flex min-w-0 flex-col gap-0.5">
              <p className="text-sm font-medium text-fg">{step.title}</p>
              <p className="text-sm text-fg-muted">{step.description}</p>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}
