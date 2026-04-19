// ChecksHistory — табличная презентация истории проверок (экран 8 Figma,
// §17.4). Использует тот же источник данных, что и VersionsTimeline
// (useVersions), но показывает версии как строки таблицы.
//
// Таблица упрощена: в v1 не включаем DataTable-sort/filter (TanStack) —
// для 5-20 строк overkill. Сортировка на уровне данных (version_number DESC).
// При необходимости — миграция на `@/shared/ui DataTable` (FE-TASK-021)
// одной строкой.
import { Link } from 'react-router-dom';

import { StatusBadge, type VersionDetails } from '@/entities/version';
import { Spinner } from '@/shared/ui';

export interface ChecksHistoryProps {
  contractId: string;
  versions: readonly VersionDetails[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const ORIGIN_LABEL: Record<NonNullable<VersionDetails['origin_type']>, string> = {
  UPLOAD: 'Загрузка',
  RE_UPLOAD: 'Повторная загрузка',
  RE_CHECK: 'Перепроверка',
  MANUAL_EDIT: 'Ручная правка',
  RECOMMENDATION_APPLIED: 'По рекомендации',
};

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric' });
}

export function ChecksHistory({
  contractId,
  versions,
  isLoading,
  error,
}: ChecksHistoryProps): JSX.Element {
  return (
    <section
      aria-label="Журнал проверок"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Журнал проверок
        </h2>
        <p className="mt-1 text-xs text-fg-muted">Статус каждой версии и ссылки на результат</p>
      </header>

      {isLoading && !versions ? (
        <div
          data-testid="checks-history-loading"
          className="flex min-h-[120px] items-center justify-center"
          aria-busy="true"
        >
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить журнал проверок.
        </p>
      ) : !versions || versions.length === 0 ? (
        <p className="text-sm text-fg-muted">Проверок пока не было.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="min-w-full text-sm" data-testid="checks-history-table">
            <thead>
              <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-fg-muted">
                <th className="pb-2 pr-3">Версия</th>
                <th className="pb-2 pr-3">Тип</th>
                <th className="pb-2 pr-3">Дата</th>
                <th className="pb-2 pr-3">Статус</th>
                <th className="pb-2" />
              </tr>
            </thead>
            <tbody>
              {[...versions]
                .sort((a, b) => (b.version_number ?? 0) - (a.version_number ?? 0))
                .map((v) => (
                  <tr
                    key={v.version_id ?? `v-${v.version_number ?? ''}`}
                    className="border-b border-border last:border-0"
                    data-testid="checks-history-row"
                  >
                    <td className="py-2 pr-3 font-medium text-fg">v{v.version_number ?? '—'}</td>
                    <td className="py-2 pr-3 text-fg-muted">
                      {v.origin_type ? ORIGIN_LABEL[v.origin_type] : '—'}
                    </td>
                    <td className="py-2 pr-3 text-fg-muted">{formatDate(v.created_at)}</td>
                    <td className="py-2 pr-3">
                      <StatusBadge status={v.processing_status ?? null} />
                    </td>
                    <td className="py-2">
                      {v.version_id ? (
                        <Link
                          to={`/contracts/${contractId}/versions/${v.version_id}/result`}
                          className="text-sm text-brand-600 hover:text-brand-500"
                        >
                          Открыть
                        </Link>
                      ) : null}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
