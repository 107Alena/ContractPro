// KeyRisksCards — агрегированные риски по последним проверкам (§17.4).
//
// Rich-данные (RiskProfile high/medium/low из /risks) требуют per-версии
// запроса — это scope FE-TASK-046 (ResultPage). На dashboard показываем
// агрегированные статусы последних 5 проверок: сколько готово с рисками,
// сколько в работе, сколько отклонено. Для BUSINESS_USER виджет скрыт
// через <Can I="risks.view"/> в DashboardPage (§5.6.1 Pattern B).
import { Link } from 'react-router-dom';

import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Badge, Spinner } from '@/shared/ui';

export interface KeyRisksCardsProps {
  items?: readonly ContractSummary[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

interface Buckets {
  ready: ContractSummary[];
  awaiting: ContractSummary[];
  failed: ContractSummary[];
}

function splitByBucket(items: readonly ContractSummary[]): Buckets {
  const out: Buckets = { ready: [], awaiting: [], failed: [] };
  for (const item of items) {
    const { bucket } = viewStatus(item.processing_status);
    if (bucket === 'ready') out.ready.push(item);
    else if (bucket === 'awaiting') out.awaiting.push(item);
    else if (bucket === 'failed') out.failed.push(item);
  }
  return out;
}

export function KeyRisksCards({ items, isLoading, error }: KeyRisksCardsProps): JSX.Element {
  return (
    <section
      aria-label="Ключевые риски"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Ключевые риски
        </h2>
        <p className="mt-1 text-xs text-fg-muted">По последним проверкам</p>
      </header>

      {isLoading && !items ? (
        <div className="flex min-h-[120px] items-center justify-center" aria-busy="true">
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить данные о рисках.
        </p>
      ) : !items || items.length === 0 ? (
        <p className="text-sm text-fg-muted">Риски появятся после первой проверки.</p>
      ) : (
        <RiskBuckets items={items} />
      )}
    </section>
  );
}

function RiskBuckets({ items }: { items: readonly ContractSummary[] }): JSX.Element {
  const buckets = splitByBucket(items);

  const groups: Array<{
    key: keyof Buckets;
    label: string;
    tone: 'success' | 'warning' | 'danger';
    hint: string;
  }> = [
    { key: 'ready', label: 'Готовы', tone: 'success', hint: 'Доступны рекомендации.' },
    {
      key: 'awaiting',
      label: 'Требуют действий',
      tone: 'warning',
      hint: 'Ожидается подтверждение типа договора.',
    },
    {
      key: 'failed',
      label: 'Проблемные',
      tone: 'danger',
      hint: 'Завершились с ошибкой или отклонены.',
    },
  ];

  return (
    <ul className="flex flex-col gap-3">
      {groups.map((group) => {
        const list = buckets[group.key];
        return (
          <li
            key={group.key}
            className="flex items-start justify-between gap-3 rounded-md border border-border bg-bg-muted p-3"
          >
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                <Badge variant={group.tone}>{list.length}</Badge>
                <span className="text-sm font-medium text-fg">{group.label}</span>
              </div>
              <p className="text-xs text-fg-muted">{group.hint}</p>
            </div>
            {list.length > 0 && list[0]?.contract_id ? (
              <Link
                to={`/contracts/${list[0].contract_id}`}
                className="shrink-0 text-sm text-brand-600 hover:text-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
              >
                Открыть
              </Link>
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}

export { splitByBucket };
