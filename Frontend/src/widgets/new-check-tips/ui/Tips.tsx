// Tips — блок «Советы для лучшего результата» на NewCheckPage
// (Figma 112:2 → 118:2, FE-TASK-043). Presentational-only: 4 статичные
// подсказки по подготовке PDF. Декоративные эмодзи — aria-hidden, чтобы
// скринридер не озвучивал их unicode-имена.
import { type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';
import { Card } from '@/shared/ui';

export interface TipsProps extends HTMLAttributes<HTMLElement> {
  className?: string;
}

const TIPS: ReadonlyArray<{ icon: string; text: string }> = [
  { icon: '📄', text: 'Убедитесь, что PDF-файл читаемый и не защищён паролем.' },
  { icon: '📋', text: 'Для точного анализа используйте полный текст договора.' },
  { icon: '📎', text: 'Если есть приложения — сначала проверьте основной договор.' },
  { icon: 'ⓘ', text: 'Итог анализа носит рекомендательный характер.' },
];

export function Tips({ className, ...rest }: TipsProps): JSX.Element {
  return (
    <Card
      aria-label="Советы для лучшего результата"
      radius="lg"
      className={cn(
        'flex flex-col gap-3.5 border border-border-subtle px-6 py-5 shadow-none',
        className,
      )}
      {...rest}
    >
      <h2 className="text-15 font-semibold text-fg">Советы для лучшего результата</h2>

      <ul className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {TIPS.map((tip) => (
          <li
            key={tip.text}
            className="flex flex-col gap-2 rounded-md bg-bg-muted p-3.5 text-13 leading-[19px] text-fg-muted"
          >
            <span aria-hidden className="text-18">
              {tip.icon}
            </span>
            {tip.text}
          </li>
        ))}
      </ul>
    </Card>
  );
}
