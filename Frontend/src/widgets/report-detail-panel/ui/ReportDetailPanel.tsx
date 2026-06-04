// ReportDetailPanel (FE-TASK-048) — боковая панель с метаданными и действиями
// выбранного отчёта на странице «Отчёты» (Figma 9, §17.4).
//
// Собирает инфо из ContractSummary (без отдельного API-вызова в v1). Действия:
//   - «Открыть результаты» → Link на /contracts/:id.
//   - «Экспорт и отправка отчёта» → открывает ExportShareModal (поднимается
//     page-компонентом через onOpenShare). Кнопка экспорта гейтится
//     useCanExport() — Pattern B §5.6.1.
//
// Dialog-контракт использован легковесно (side-panel, а не модалка):
//   `<aside>` даёт неявную complementary-role + aria-labelledby. Focus-trap не
//   нужен — пользователь должен иметь возможность уйти клавиатурой на другую
//   строку таблицы. Закрытие — кнопкой «Закрыть» или Escape.
import { useEffect, useRef } from 'react';
import { Link } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { type RiskLevel, riskLevelMeta } from '@/entities/risk';
import { StatusBadge } from '@/entities/version';
import { useCanExport } from '@/shared/auth/use-can-export';
import { Button, buttonVariants, Spinner } from '@/shared/ui';

/** Компактный профиль рисков выбранного отчёта (page агрегирует через useRisks). */
export interface ReportRiskProfileView {
  /**
   * Authoritative-вердикт бэка (overall_level). null, если бэк уровень не вынес —
   * тогда показываем нейтральный заголовок + счётчики, вердикт НЕ выдумываем.
   */
  level: RiskLevel | null;
  high: number;
  medium: number;
  low: number;
}

// Статичные строки классов (не шаблон) — иначе Tailwind JIT не сгенерирует.
const RISK_DOT: Record<RiskLevel, string> = {
  high: 'bg-risk-high',
  medium: 'bg-risk-medium',
  low: 'bg-risk-low',
};

type RiskCountKey = 'high' | 'medium' | 'low';

const RISK_BREAKDOWN: ReadonlyArray<readonly [RiskCountKey, string]> = [
  ['high', 'высоких'],
  ['medium', 'средних'],
  ['low', 'низких'],
];

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString('ru-RU', {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export interface ReportDetailPanelProps {
  contract: ContractSummary | null;
  /**
   * UUID текущей версии выбранного отчёта (резолвится page'ом через useContract).
   * Реестр отдаёт только current_version_number — для экспорта/share нужен UUID.
   * Пока детали грузятся — undefined/null, кнопка экспорта disabled.
   */
  versionId?: string | null;
  /** Профиль рисков версии (page агрегирует useRisks → toReportRiskProfile). */
  riskProfile?: ReportRiskProfileView | null;
  /** Риск-данные ещё грузятся (детали версии или /risks). */
  riskLoading?: boolean;
  /** Показывать риск-секцию (есть право risks.view). BUSINESS_USER → false. */
  showRisk?: boolean;
  onClose: () => void;
  /** Открывает ExportShareModal на уровне page. Вызывается с contractId+versionId. */
  onOpenShare?: (input: { contractId: string; versionId: string }) => void;
}

export function ReportDetailPanel({
  contract,
  versionId,
  riskProfile,
  riskLoading,
  showRisk,
  onClose,
  onOpenShare,
}: ReportDetailPanelProps): JSX.Element | null {
  const canExport = useCanExport();
  const titleId = 'report-detail-panel-title';
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // Escape to close. При смене выбранной строки — focus на кнопку закрытия,
  // чтобы скринридер проговорил смену контекста.
  useEffect(() => {
    if (!contract) return;
    closeBtnRef.current?.focus();

    function onKeyDown(e: KeyboardEvent): void {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return (): void => window.removeEventListener('keydown', onKeyDown);
  }, [contract, onClose]);

  if (!contract) return null;

  const title = contract.title ?? 'Без названия';
  const version = contract.current_version_number;
  const updatedAt = contract.updated_at ?? contract.created_at;
  const canOpenShare =
    canExport && onOpenShare != null && contract.contract_id != null && versionId != null;

  const handleOpenShare = (): void => {
    if (!canOpenShare) return;
    onOpenShare!({
      contractId: contract.contract_id as string,
      versionId: versionId as string,
    });
  };

  return (
    <aside
      aria-labelledby={titleId}
      data-testid="report-detail-panel"
      className="flex w-full max-w-sm flex-col gap-4 rounded-xl border border-border-subtle bg-bg p-4 shadow-none"
    >
      <header className="flex items-start justify-between gap-3">
        <div>
          <h2 id={titleId} className="text-18 font-semibold text-fg">
            {title}
          </h2>
          <p className="mt-1 text-13 text-fg-muted">Детали отчёта</p>
        </div>
        <Button
          ref={closeBtnRef}
          type="button"
          variant="ghost"
          size="sm"
          onClick={onClose}
          data-testid="report-detail-panel-close"
          aria-label="Закрыть панель деталей"
        >
          Закрыть
        </Button>
      </header>

      {showRisk ? (
        <div
          data-testid="report-detail-risk"
          className="flex flex-col gap-3 rounded-lg border border-border-subtle bg-bg-muted p-4"
        >
          {riskLoading ? (
            <div
              data-testid="report-detail-risk-loading"
              aria-busy="true"
              className="flex items-center gap-2 text-sm text-fg-muted"
            >
              <Spinner size="sm" aria-hidden="true" />
              <span>Загружаем профиль рисков…</span>
            </div>
          ) : riskProfile ? (
            <>
              {riskProfile.level ? (
                <div className="flex items-center gap-2">
                  <span
                    aria-hidden="true"
                    className={`inline-block h-2 w-2 rounded-full ${RISK_DOT[riskProfile.level]}`}
                  />
                  <span
                    className="text-15 font-semibold text-fg"
                    data-testid="report-detail-risk-level"
                  >
                    {riskLevelMeta(riskProfile.level).label}
                  </span>
                </div>
              ) : (
                <span
                  className="text-15 font-semibold text-fg"
                  data-testid="report-detail-risk-level"
                >
                  Профиль рисков
                </span>
              )}
              <ul
                className="flex flex-wrap gap-x-5 gap-y-1.5 text-sm"
                aria-label="Распределение рисков"
              >
                {RISK_BREAKDOWN.map(([key, word]) => (
                  <li key={key} className="flex items-center gap-1.5">
                    <span
                      aria-hidden="true"
                      className={`inline-block h-1.5 w-1.5 rounded-full ${RISK_DOT[key]}`}
                    />
                    <span>
                      <span className="font-semibold text-fg">{riskProfile[key]}</span>{' '}
                      <span className="text-fg-muted">{word}</span>
                    </span>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <p className="text-sm text-fg-muted" data-testid="report-detail-risk-empty">
              Профиль рисков недоступен для этой версии.
            </p>
          )}
        </div>
      ) : null}

      <dl className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <dt className="text-xs text-fg-muted">Версия</dt>
          <dd className="mt-1 text-fg">{version != null ? `v${version}` : '—'}</dd>
        </div>
        <div>
          <dt className="text-xs text-fg-muted">Обновлён</dt>
          <dd className="mt-1 text-fg">{formatDateTime(updatedAt)}</dd>
        </div>
        <div className="col-span-2">
          <dt className="text-xs text-fg-muted">Статус обработки</dt>
          <dd className="mt-1">
            <StatusBadge status={contract.processing_status} />
          </dd>
        </div>
      </dl>

      <div className="flex flex-col gap-2">
        {contract.contract_id ? (
          <Link
            to={
              versionId
                ? `/contracts/${encodeURIComponent(contract.contract_id)}/versions/${encodeURIComponent(versionId)}/result`
                : `/contracts/${encodeURIComponent(contract.contract_id)}`
            }
            className={buttonVariants({ variant: 'primary', size: 'md' })}
            data-testid="report-detail-panel-open"
          >
            Открыть результаты
          </Link>
        ) : null}
        <Button
          type="button"
          variant="secondary"
          size="md"
          disabled={!canOpenShare}
          onClick={handleOpenShare}
          data-testid="report-detail-panel-share"
        >
          {canExport ? 'Экспорт и отправка отчёта' : 'Экспорт недоступен'}
        </Button>
        {!canExport ? (
          <p className="text-xs text-fg-muted">
            Экспорт и отправка доступны юристам и администраторам организации.
          </p>
        ) : null}
      </div>
    </aside>
  );
}
