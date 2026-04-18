// QuickStart — CTA-блок на dashboard: краткие шаги + кнопка «Загрузить договор».
// RBAC: скрывается, если у роли нет permission `contract.upload` (ORG_ADMIN).
// RBAC-фильтрация выполняется на уровне page через <Can/>, виджет сам
// визуально не ограничен.
import { Link } from 'react-router-dom';

import { buttonVariants } from '@/shared/ui';

export interface QuickStartProps {
  className?: string;
}

const STEPS: Array<{ title: string; description: string }> = [
  {
    title: '1. Загрузите PDF',
    description: 'Перетащите файл или выберите на компьютере — до 20 МБ.',
  },
  { title: '2. Дождитесь анализа', description: 'Обычно обработка занимает 1–2 минуты.' },
  { title: '3. Получите рекомендации', description: 'Резюме на простом языке и подсветка рисков.' },
];

export function QuickStart({ className }: QuickStartProps): JSX.Element {
  return (
    <section
      aria-label="Быстрый старт"
      className={[
        'flex flex-col gap-4 rounded-md border border-border bg-brand-50 p-5 shadow-sm',
        className ?? '',
      ].join(' ')}
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-brand-600">
          Быстрый старт
        </h2>
        <p className="mt-1 text-base font-semibold text-fg">Проверьте новый договор</p>
      </header>

      <ol className="flex flex-col gap-2">
        {STEPS.map((step) => (
          <li key={step.title} className="text-sm text-fg">
            <p className="font-medium">{step.title}</p>
            <p className="text-fg-muted">{step.description}</p>
          </li>
        ))}
      </ol>

      <Link
        to="/contracts/new"
        className={`${buttonVariants({ variant: 'primary', size: 'md' })} self-start`}
      >
        Загрузить договор
      </Link>
    </section>
  );
}
