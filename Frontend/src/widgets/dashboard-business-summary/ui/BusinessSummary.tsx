// BusinessSummary — карточка «Сводка» на dashboard (Figma 84:2 → 91:2).
//
// Две метрики: «проверено» = total из /contracts (реальный all-time счётчик) и
// «в работе» — all-time счётчик договоров в обработке. Агрегата по статусам в
// API нет (ContractList отдаёт только total; фильтр /contracts — по
// DocumentStatus, не по processing-статусу), поэтому «в работе» рендерится как
// «—» до появления aggregate-эндпоинта. Никаких выдуманных чисел.
import { Card, Spinner } from '@/shared/ui';

export interface BusinessSummaryProps {
  total?: number | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

export function BusinessSummary({ total, isLoading, error }: BusinessSummaryProps): JSX.Element {
  return (
    <Card as="article" aria-label="Сводка" className="flex flex-col gap-3.5 p-5">
      <h2 className="text-15 font-semibold text-fg">Сводка</h2>

      {isLoading && total === undefined ? (
        <div className="flex min-h-[60px] items-center justify-center" aria-busy="true">
          <Spinner size="sm" aria-hidden="true" />
          <span className="sr-only">Загрузка…</span>
        </div>
      ) : error ? (
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить сводку.
        </p>
      ) : (
        <>
          <div className="flex items-start">
            <Stat value={total ?? '—'} label="проверено" muted={total === undefined} />
            <Stat value="—" label="в работе" muted />
          </div>
          <div className="h-px w-full bg-divider" />
          <p className="text-13 leading-[19px] text-fg-muted">
            «В работе» появится, когда добавим агрегатную статистику по статусам договоров.
          </p>
        </>
      )}
    </Card>
  );
}

function Stat({
  value,
  label,
  muted,
}: {
  value: number | string;
  label: string;
  muted?: boolean;
}): JSX.Element {
  return (
    <div className="flex flex-1 flex-col items-center gap-0.5">
      <span className={`text-24 font-bold ${muted ? 'text-fg-disabled' : 'text-fg'}`}>{value}</span>
      <span className="text-11 text-fg-subtle">{label}</span>
    </div>
  );
}
