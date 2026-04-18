// WhatWeCheck — информационный блок «Что мы проверяем» на NewCheckPage
// (§17.4, FE-TASK-043). Presentational-only: bullet-list категорий проверок
// — мостик между пользователем и product-value (ТЗ-1, FR-2.1/2.2).
// Источник категорий: ТЗ-1 / domain-decomposition.md § Legal Intelligence Core.
import { type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

export interface WhatWeCheckProps extends HTMLAttributes<HTMLElement> {
  className?: string;
}

const CHECKS: Array<{ title: string; description: string }> = [
  {
    title: 'Обязательные условия',
    description: 'Предмет договора, цена, сроки, стороны и реквизиты по ГК РФ.',
  },
  {
    title: 'Юридические и финансовые риски',
    description: 'Неустойки, ответственность, расторжение, форс-мажор, валютные оговорки.',
  },
  {
    title: 'Отклонения от политики организации',
    description: 'Сверка с вашими шаблонами и чек-листами (если настроены).',
  },
  {
    title: 'Рекомендации по формулировкам',
    description: 'Конкретные улучшения спорных и размытых формулировок.',
  },
];

export function WhatWeCheck({ className, ...rest }: WhatWeCheckProps): JSX.Element {
  return (
    <section
      aria-label="Что мы проверяем"
      className={cn(
        'flex flex-col gap-4 rounded-md border border-border bg-bg p-5 shadow-sm',
        className,
      )}
      {...rest}
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Что мы проверяем
        </h2>
        <p className="mt-1 text-base font-semibold text-fg">Полный юридический контур</p>
      </header>

      <ul className="flex flex-col gap-3">
        {CHECKS.map((check) => (
          <li key={check.title} className="flex items-start gap-3">
            <span
              aria-hidden
              className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-brand-500"
            />
            <div className="flex min-w-0 flex-col gap-0.5">
              <p className="text-sm font-medium text-fg">{check.title}</p>
              <p className="text-sm text-fg-muted">{check.description}</p>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
