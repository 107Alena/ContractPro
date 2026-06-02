// WhatWeCheck — информационный блок «Что проверяет ContractPro» на
// NewCheckPage (Figma 112:2 → 117:35, FE-TASK-043). Presentational-only:
// wrap-сетка тегов категорий проверки + дисклеймер рекомендательного
// характера. Источник категорий: ТЗ-1 / domain-decomposition.md
// § Legal Intelligence Core (все категории — реальные проверки, не плейсхолдер).
import { type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';
import { Card } from '@/shared/ui';

export interface WhatWeCheckProps extends HTMLAttributes<HTMLElement> {
  className?: string;
}

const CHECKS: readonly string[] = [
  'Стороны договора',
  'Предмет договора',
  'Цена и расчёты',
  'Сроки действия',
  'Ответственность',
  'Неустойки',
  'Порядок приёмки',
  'Качество',
  'Расторжение',
  'Подсудность',
  'Применимое право',
  'Отклонения от шаблона',
  'Риск-конструкции',
  'Реквизиты сторон',
];

export function WhatWeCheck({ className, ...rest }: WhatWeCheckProps): JSX.Element {
  return (
    <Card
      aria-label="Что проверяет ContractPro"
      radius="lg"
      className={cn('flex flex-col gap-4 border border-border-subtle p-6 shadow-none', className)}
      {...rest}
    >
      <h2 className="text-16 font-semibold text-fg">Что проверяет ContractPro</h2>

      <ul className="flex flex-wrap gap-2">
        {CHECKS.map((label) => (
          <li
            key={label}
            className="inline-flex items-center gap-1.5 rounded-md bg-bg-muted px-3 py-1.5 text-13 font-medium text-fg-strong"
          >
            <span aria-hidden className="text-11 text-brand-500">
              ✓
            </span>
            {label}
          </li>
        ))}
      </ul>

      <p className="text-12 leading-[18px] text-fg-disabled">
        Анализ носит рекомендательный характер и не является юридическим заключением.
      </p>
    </Card>
  );
}
