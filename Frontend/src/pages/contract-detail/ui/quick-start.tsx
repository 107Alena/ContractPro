// QuickStart (ContractDetail) — CTA «Загрузить новую версию» на карточке
// договора. Отличается от widgets/dashboard-quick-start (там CTA на первую
// загрузку). Feature version-upload существует, но его мутация монтируется
// отдельной страницей /contracts/new (в v1). Здесь — ссылка-CTA, без
// локальной мутации.
import { Link } from 'react-router-dom';

import { buttonVariants } from '@/shared/ui';

export interface QuickStartProps {
  contractId: string;
}

const STEPS: Array<{ title: string; description: string }> = [
  { title: '1. Загрузите PDF', description: 'Перетащите новую редакцию или выберите файл.' },
  { title: '2. Дождитесь анализа', description: 'Обычно проверка занимает 1–2 минуты.' },
  { title: '3. Сверьте версии', description: 'Сравните изменения и применяйте рекомендации.' },
];

export function QuickStart({ contractId }: QuickStartProps): JSX.Element {
  return (
    <section
      aria-label="Что дальше"
      className="flex flex-col gap-4 rounded-md border border-border bg-brand-50 p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-brand-600">Что дальше</h2>
        <p className="mt-1 text-base font-semibold text-fg">Загрузите новую версию</p>
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
        to={`/contracts/new?contractId=${contractId}`}
        className={`${buttonVariants({ variant: 'primary', size: 'md' })} self-start`}
      >
        Загрузить новую версию
      </Link>
    </section>
  );
}
