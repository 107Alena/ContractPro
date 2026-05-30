// CurrentActions — секция «Что важно сейчас» на dashboard (Figma 84:2 → 88:2).
//
// Actionable-карточки договоров, требующих внимания, из РЕАЛЬНОГО processing_status
// (в обработке / требуется действие / ошибка). Figma-карточка «ВЫСОКИЙ РИСК»
// опущена — risk-level недоступен (приходит из /risks, FE-TASK-046). KPI-итоги
// дашборда живут в карточке «Сводка» (BusinessSummary), а не здесь.
//
// Отдельный виджет от dashboard-what-matters (KPI-счётчики): тот переиспользуется
// на /contracts, поэтому не репёрпозится.
import { Link } from 'react-router-dom';

import { type ContractSummary, type StatusBucket, viewStatus } from '@/entities/contract';
import { Card, Spinner } from '@/shared/ui';

export interface CurrentActionsProps {
  items?: readonly ContractSummary[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

type ActionKind = 'processing' | 'awaiting' | 'failed';

export interface ActionItem {
  contract: ContractSummary;
  kind: ActionKind;
}

const ACTION_META: Record<
  ActionKind,
  { caption: string; desc: string; cta: string; dot: string; captionColor: string }
> = {
  processing: {
    caption: 'В ОБРАБОТКЕ',
    desc: 'Анализ рисков выполняется. Обновим результат автоматически.',
    cta: 'Следить за статусом →',
    dot: 'bg-processing',
    captionColor: 'text-processing',
  },
  awaiting: {
    caption: 'ТРЕБУЕТСЯ ДЕЙСТВИЕ',
    desc: 'Система не определила тип договора. Подтвердите классификацию.',
    cta: 'Подтвердить тип →',
    dot: 'bg-brand-500',
    captionColor: 'text-brand-600',
  },
  failed: {
    caption: 'ОШИБКА',
    desc: 'Обработка завершилась с ошибкой. Откройте карточку для деталей.',
    cta: 'Открыть договор →',
    dot: 'bg-danger',
    captionColor: 'text-danger',
  },
};

const BUCKET_TO_KIND: Partial<Record<StatusBucket, ActionKind>> = {
  in_progress: 'processing',
  awaiting: 'awaiting',
  failed: 'failed',
};

/** Отбирает до `limit` договоров, требующих внимания, и размечает их по виду действия. */
export function selectActionItems(items: readonly ContractSummary[], limit = 3): ActionItem[] {
  const out: ActionItem[] = [];
  for (const contract of items) {
    const kind = BUCKET_TO_KIND[viewStatus(contract.processing_status).bucket];
    if (kind) out.push({ contract, kind });
    if (out.length >= limit) break;
  }
  return out;
}

export function CurrentActions({ items, isLoading, error }: CurrentActionsProps): JSX.Element {
  if (isLoading && !items) {
    return (
      <section
        aria-label="Что важно сейчас"
        aria-busy="true"
        className="flex min-h-[120px] items-center justify-center rounded-[12px] bg-bg-muted"
      >
        <Spinner size="md" aria-hidden="true" />
      </section>
    );
  }

  if (error) {
    return (
      <section aria-label="Что важно сейчас" className="flex flex-col gap-4">
        <h2 className="text-17 font-semibold text-fg">Что важно сейчас</h2>
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить список задач.
        </p>
      </section>
    );
  }

  const actions = selectActionItems(items ?? []);

  return (
    <section aria-label="Что важно сейчас" className="flex flex-col gap-4">
      <h2 className="text-17 font-semibold text-fg">Что важно сейчас</h2>
      {actions.length === 0 ? (
        <Card className="px-5 py-[18px]">
          <p className="text-13 text-fg-muted">Сейчас нет проверок, требующих внимания.</p>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          {actions.map((item, index) => (
            <ActionCard key={item.contract.contract_id ?? `_${index}`} item={item} />
          ))}
        </div>
      )}
    </section>
  );
}

function ActionCard({ item }: { item: ActionItem }): JSX.Element {
  const meta = ACTION_META[item.kind];
  const { contract } = item;

  return (
    <Card as="article" className="flex flex-col gap-2.5 px-5 py-[18px]">
      <div className="flex items-center gap-2">
        <span className={`size-2 rounded-full ${meta.dot}`} aria-hidden="true" />
        <span className={`text-11 font-semibold uppercase tracking-[0.3px] ${meta.captionColor}`}>
          {meta.caption}
        </span>
      </div>
      <p className="text-14 font-semibold text-fg">{contract.title ?? 'Договор без названия'}</p>
      <p className="text-13 text-fg-muted">{meta.desc}</p>
      {contract.contract_id ? (
        <Link
          to={`/contracts/${contract.contract_id}`}
          className="text-13 font-medium text-brand-600 hover:text-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
        >
          {meta.cta}
        </Link>
      ) : null}
    </Card>
  );
}
