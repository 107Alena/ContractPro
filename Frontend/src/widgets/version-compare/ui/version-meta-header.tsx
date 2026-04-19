// VersionMetaHeader — мета двух версий: «base | target».
// На md+ — две колонки бок о бок, на sm — друг под другом (стек).
// Если versionId не передан, показываем плейсхолдер «Версия не выбрана».
import { cn } from '@/shared/lib/cn';

import type { VersionMetadata } from '../model/types';

export interface VersionMetaHeaderProps {
  base?: VersionMetadata;
  target?: VersionMetadata;
  className?: string;
}

function formatDate(iso?: string): string | undefined {
  if (!iso) return undefined;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return undefined;
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: 'long', year: 'numeric' });
}

interface VersionColumnProps {
  label: string;
  meta: VersionMetadata | undefined;
  testId: string;
}

function VersionColumn({ label, meta, testId }: VersionColumnProps) {
  const formattedDate = formatDate(meta?.createdAt);
  return (
    <div
      className="flex flex-col gap-1 rounded-md border border-border bg-bg p-4"
      data-testid={testId}
    >
      <p className="text-xs font-medium uppercase tracking-wide text-fg-muted">{label}</p>
      {meta ? (
        <>
          <h3 className="text-base font-semibold text-fg">
            {meta.versionNumber !== undefined ? `v${meta.versionNumber}` : 'Версия'}
            {meta.title ? (
              <span className="ml-2 font-normal text-fg-muted">{meta.title}</span>
            ) : null}
          </h3>
          <dl className="mt-1 grid grid-cols-[max-content,1fr] gap-x-3 gap-y-1 text-xs text-fg-muted">
            {meta.authorName ? (
              <>
                <dt>Автор:</dt>
                <dd className="text-fg">{meta.authorName}</dd>
              </>
            ) : null}
            {formattedDate ? (
              <>
                <dt>Создана:</dt>
                <dd className="text-fg">{formattedDate}</dd>
              </>
            ) : null}
          </dl>
        </>
      ) : (
        <p className="text-sm text-fg-muted">Версия не выбрана</p>
      )}
    </div>
  );
}

export function VersionMetaHeader({
  base,
  target,
  className,
}: VersionMetaHeaderProps): JSX.Element {
  return (
    <section
      aria-label="Метаданные сравниваемых версий"
      data-testid="version-meta-header"
      className={cn('grid gap-3 md:grid-cols-2', className)}
    >
      <VersionColumn label="Базовая версия" meta={base} testId="version-meta-header-base" />
      <VersionColumn label="Целевая версия" meta={target} testId="version-meta-header-target" />
    </section>
  );
}
