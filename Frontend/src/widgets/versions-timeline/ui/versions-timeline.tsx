// VersionsTimeline — вертикальный таймлайн версий договора на карточке документа
// (экран 8 Figma, §17.4). Один источник данных — `useVersions(contractId)`.
// Parent-страница передаёт массив; виджет визуализирует порядок создания
// (v1 → v2 → …) с origin_type, датой и StatusBadge.
//
// ChecksHistory (второй слот из §17.4) реализован в соседнем компоненте
// `checks-history.tsx` — та же коллекция, но табличная презентация с DataTable.
import { Link } from 'react-router-dom';

import { StatusBadge, type VersionDetails } from '@/entities/version';
import { Spinner } from '@/shared/ui';

export interface VersionsTimelineProps {
  contractId: string;
  versions: readonly VersionDetails[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const ORIGIN_LABEL: Record<NonNullable<VersionDetails['origin_type']>, string> = {
  UPLOAD: 'Первичная загрузка',
  RE_UPLOAD: 'Новая редакция',
  RE_CHECK: 'Перепроверка',
  MANUAL_EDIT: 'Ручная правка',
  RECOMMENDATION_APPLIED: 'По рекомендации',
};

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: 'long', year: 'numeric' });
}

export function VersionsTimeline({
  contractId,
  versions,
  isLoading,
  error,
}: VersionsTimelineProps): JSX.Element {
  return (
    <section
      aria-label="История версий"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          История версий
        </h2>
        <p className="mt-1 text-xs text-fg-muted">Хронология загрузок и перепроверок</p>
      </header>

      {isLoading && !versions ? (
        <div
          data-testid="versions-timeline-loading"
          className="flex min-h-[120px] items-center justify-center"
          aria-busy="true"
        >
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить список версий.
        </p>
      ) : !versions || versions.length === 0 ? (
        <p className="text-sm text-fg-muted">Версий пока нет — загрузите первую редакцию.</p>
      ) : (
        <ol className="flex flex-col" data-testid="versions-timeline-list">
          {[...versions]
            .sort((a, b) => (b.version_number ?? 0) - (a.version_number ?? 0))
            .map((v, idx, arr) => (
              <TimelineItem
                key={v.version_id ?? `${v.version_number ?? idx}`}
                contractId={contractId}
                version={v}
                isLast={idx === arr.length - 1}
              />
            ))}
        </ol>
      )}
    </section>
  );
}

interface TimelineItemProps {
  contractId: string;
  version: VersionDetails;
  isLast: boolean;
}

function TimelineItem({ contractId, version, isLast }: TimelineItemProps): JSX.Element {
  const originLabel = version.origin_type ? ORIGIN_LABEL[version.origin_type] : '';
  const vid = version.version_id;

  return (
    <li className="relative flex gap-3 pb-4" data-testid="versions-timeline-item">
      <div className="flex flex-col items-center">
        <span
          aria-hidden="true"
          className="mt-1 h-2 w-2 rounded-full bg-brand-600 ring-2 ring-brand-100"
        />
        {!isLast ? <span aria-hidden="true" className="mt-1 w-px flex-1 bg-border" /> : null}
      </div>
      <div className="flex flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">v{version.version_number ?? '—'}</span>
          <StatusBadge status={version.processing_status ?? null} />
          {originLabel ? <span className="text-xs text-fg-muted">· {originLabel}</span> : null}
        </div>
        {version.source_file_name ? (
          <p className="text-sm text-fg-muted">{version.source_file_name}</p>
        ) : null}
        <p className="text-xs text-fg-muted">{formatDate(version.created_at)}</p>
        {vid ? (
          <Link
            to={`/contracts/${contractId}/versions/${vid}/result`}
            className="text-sm text-brand-600 hover:text-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
          >
            Открыть результат
          </Link>
        ) : null}
      </div>
    </li>
  );
}
