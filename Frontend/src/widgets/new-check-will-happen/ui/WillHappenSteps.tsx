// WillHappenSteps — информационный блок «Что произойдёт после запуска» на
// NewCheckPage (Figma 112:2 → 117:3, FE-TASK-043). Presentational-only:
// нумерованный список из 4 шагов обработки договора с вертикальными
// коннекторами между ними. Источник шагов: §5.2 (pipeline)
// ApiBackendOrchestrator/architecture, копирайт — по Figma.
import { type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';
import { Card } from '@/shared/ui';

export interface WillHappenStepsProps extends HTMLAttributes<HTMLElement> {
  className?: string;
}

const STEPS: Array<{ title: string; description: string }> = [
  {
    title: 'Извлечение текста',
    description: 'ContractPro прочитает и структурирует содержание документа.',
  },
  {
    title: 'Определение типа',
    description: 'Система определит тип договора и его ключевые параметры.',
  },
  {
    title: 'Проверка условий и рисков',
    description: 'Анализ обязательных условий, юридических и финансовых рисков.',
  },
  {
    title: 'Рекомендации и сводка',
    description: 'Формирование рекомендаций и краткого резюме простым языком.',
  },
];

export function WillHappenSteps({ className, ...rest }: WillHappenStepsProps): JSX.Element {
  return (
    <Card
      aria-label="Что произойдёт после запуска"
      radius="lg"
      className={cn('flex flex-col gap-4 border border-border-subtle p-6 shadow-none', className)}
      {...rest}
    >
      <h2 className="text-16 font-semibold text-fg">Что произойдёт после запуска</h2>

      <ol className="flex flex-col">
        {STEPS.map((step, index) => (
          <li key={step.title} className="flex flex-col">
            <div className="flex items-start gap-3">
              <span
                aria-hidden
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-brand-500/10 text-13 font-semibold text-brand-500"
              >
                {index + 1}
              </span>
              <div className="flex min-w-0 flex-col gap-1">
                <p className="text-14 font-medium text-fg">{step.title}</p>
                <p className="text-13 leading-[19px] text-fg-subtle">{step.description}</p>
              </div>
            </div>
            {index < STEPS.length - 1 ? (
              <span aria-hidden className="ml-[13px] h-3 w-0.5 rounded-sm bg-border-subtle" />
            ) : null}
          </li>
        ))}
      </ol>
    </Card>
  );
}
