// MandatoryConditionsChecklist — виджет «Обязательные условия»
// (экран 5 Figma «Результат»). Показывает статус проверки существенных
// условий договора по чек-листу политики организации (OPM) для LAWYER/
// ORG_ADMIN. Источник данных — бэкенд формирует список на основе
// ORG_CHECKLIST + RISK_ANALYSIS; фронт в v1 принимает `items` пропом.
// BUSINESS_USER не видит виджет (гейтинг на уровне page).
import { Badge } from '@/shared/ui/badge';
import { Spinner } from '@/shared/ui/spinner';

export type MandatoryConditionStatus = 'ok' | 'warning' | 'missing';

export interface MandatoryConditionItem {
  /** Стабильный ключ для React, пробрасывается бэкендом. */
  id: string;
  /** Название условия — «Срок оплаты», «Ответственность сторон» и т.п. */
  label: string;
  /** Финальный статус проверки условия по политике. */
  status: MandatoryConditionStatus;
  /** Короткое пояснение (рекомендация по конкретному условию). */
  detail?: string;
}

export interface MandatoryConditionsChecklistProps {
  items?: readonly MandatoryConditionItem[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const STATUS_META: Record<
  MandatoryConditionStatus,
  { label: string; tone: 'success' | 'warning' | 'danger' }
> = {
  ok: { label: 'Соблюдено', tone: 'success' },
  warning: { label: 'Требует внимания', tone: 'warning' },
  missing: { label: 'Отсутствует', tone: 'danger' },
};

export function MandatoryConditionsChecklist({
  items,
  isLoading,
  error,
}: MandatoryConditionsChecklistProps): JSX.Element {
  return (
    <section
      aria-label="Обязательные условия"
      data-testid="mandatory-conditions"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Обязательные условия
        </h2>
        <p className="text-xs text-fg-muted">
          Сверка с чек-листом организации (политики и шаблоны договоров)
        </p>
      </header>

      {isLoading && !items ? (
        <div
          data-testid="mandatory-conditions-loading"
          className="flex min-h-[120px] items-center justify-center"
          aria-busy="true"
        >
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить список обязательных условий.
        </p>
      ) : !items || items.length === 0 ? (
        <p className="text-sm text-fg-muted" data-testid="mandatory-conditions-empty">
          Обязательные условия появятся после завершения анализа.
        </p>
      ) : (
        <ul className="flex flex-col gap-2" data-testid="mandatory-conditions-list">
          {items.map((item) => (
            <ConditionRow key={item.id} item={item} />
          ))}
        </ul>
      )}
    </section>
  );
}

function ConditionRow({ item }: { item: MandatoryConditionItem }): JSX.Element {
  const meta = STATUS_META[item.status];
  return (
    <li
      data-testid={`mandatory-condition-${item.status}`}
      className="flex flex-col gap-1 rounded-md border border-border bg-bg-muted p-3"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-sm font-medium text-fg">{item.label}</p>
        <Badge variant={meta.tone}>{meta.label}</Badge>
      </div>
      {item.detail ? <p className="text-xs text-fg-muted">{item.detail}</p> : null}
    </li>
  );
}
