// QuickStart (ContractDetail) — карточка «Быстрые действия» правой колонки
// (Figma 306:2 → Quick Start Card 312:3). Набор навигационных действий по
// договору. Действия, требующие готового результата, при не-READY рендерятся
// как реальные disabled-кнопки (в tab-/a11y-дереве, со sr-only-пояснением —
// не только цветом). Действия экспорта/шаринга скрыты для ролей без права
// экспорта (useCanExport, как ExportShareButton на ResultPage) — чтобы CTA не
// обещал недоступное.
//
// «Открыть историю проверок» — якорь на секцию журнала на этой же странице
// (#check-history). Stats/Activity-карточки Figma (12 проверок, 8 рисков,
// лента активности) НЕ реализованы: бэкенда для агрегатов/ленты нет вообще —
// не выдумываем (см. scope-решение 4.7).
import { Link } from 'react-router-dom';

import { useCanExport } from '@/shared/auth';
import { cn } from '@/shared/lib/cn';
import { Card } from '@/shared/ui';

export interface QuickStartProps {
  contractId: string;
  versionId?: string | undefined;
  isReady?: boolean;
}

interface ActionDef {
  icon: string;
  label: string;
  href?: string | undefined;
  requiresExport?: boolean;
}

export function QuickStart({
  contractId,
  versionId,
  isReady = false,
}: QuickStartProps): JSX.Element {
  const canExport = useCanExport();
  const resultHref =
    isReady && versionId ? `/contracts/${contractId}/versions/${versionId}/result` : undefined;

  const actions: ActionDef[] = [
    { icon: '→', label: 'Открыть результат проверки', href: resultHref },
    { icon: '⇆', label: 'Сравнить версии', href: `/contracts/${contractId}/compare` },
    { icon: '↑', label: 'Загрузить новую версию', href: `/contracts/new?contractId=${contractId}` },
    { icon: '↓', label: 'Скачать последний отчёт', href: resultHref, requiresExport: true },
    { icon: '🔗', label: 'Поделиться ссылкой', href: resultHref, requiresExport: true },
    { icon: '◎', label: 'Открыть историю проверок', href: '#check-history' },
  ].filter((a) => !a.requiresExport || canExport);

  return (
    <Card
      as="section"
      aria-label="Быстрые действия"
      radius="lg"
      className="flex flex-col gap-1 border border-border-subtle p-5 shadow-none"
    >
      <h2 className="mb-2 text-16 font-semibold text-fg">Быстрые действия</h2>
      <ul className="flex flex-col">
        {actions.map((a) => (
          <li key={a.label}>
            <ActionRow {...a} />
          </li>
        ))}
      </ul>
    </Card>
  );
}

const ROW_BASE = 'flex w-full items-center gap-2 rounded-md py-2 text-left text-14';
const ROW_INTERACTIVE =
  'text-fg transition-colors hover:text-brand-600 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1';

function ActionRow({ icon, label, href }: ActionDef): JSX.Element {
  const inner = (
    <>
      <span aria-hidden className="w-4 shrink-0 text-center text-13">
        {icon}
      </span>
      {label}
    </>
  );

  if (!href) {
    return (
      <button
        type="button"
        disabled
        className={cn(ROW_BASE, 'cursor-not-allowed text-fg-disabled')}
      >
        {inner}
        <span className="sr-only"> — недоступно до завершения анализа</span>
      </button>
    );
  }
  if (href.startsWith('#')) {
    return (
      <a href={href} className={cn(ROW_BASE, ROW_INTERACTIVE)}>
        {inner}
      </a>
    );
  }
  return (
    <Link to={href} className={cn(ROW_BASE, ROW_INTERACTIVE)}>
      {inner}
    </Link>
  );
}
