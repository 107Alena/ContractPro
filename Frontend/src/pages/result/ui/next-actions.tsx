// NextActions — виджет «Следующие шаги» (§16.5 дерево ResultPage).
// Формирует актуальные CTA по рисковому профилю договора: отсутствуют —
// можно экспортировать; есть медиум/хай — сначала запустить правки.
// Вход: агрегированные данные AnalysisResults.
import type { ReactNode } from 'react';

import type { AnalysisResults } from '@/entities/result';
import { isPolicyDeviation } from '@/entities/risk';

export interface NextActionsProps {
  results: AnalysisResults;
}

interface ActionItem {
  id: string;
  title: string;
  description: ReactNode;
}

function buildActions(results: AnalysisResults): ActionItem[] {
  const profile = results.risk_profile;
  const high = profile?.high_count ?? 0;
  const medium = profile?.medium_count ?? 0;
  const recommendations = results.recommendations ?? [];
  const hasDeviations = (results.risks ?? []).some(isPolicyDeviation);

  const actions: ActionItem[] = [];

  if (high > 0) {
    actions.push({
      id: 'resolve-high',
      title: 'Устраните высокие риски',
      description: `Высоких рисков: ${high}. Согласуйте правки с контрагентом перед подписанием.`,
    });
  }

  if (medium > 0) {
    actions.push({
      id: 'review-medium',
      title: 'Проверьте средние риски',
      description: `Средних рисков: ${medium}. Уточните формулировки в спорных пунктах.`,
    });
  }

  if (recommendations.length > 0) {
    actions.push({
      id: 'apply-recommendations',
      title: 'Примените рекомендованные формулировки',
      description: `Подготовлено рекомендаций: ${recommendations.length}. Скопируйте готовый текст в договор.`,
    });
  }

  if (hasDeviations) {
    actions.push({
      id: 'policy-deviations',
      title: 'Согласуйте отклонения от политики',
      description:
        'Обнаружены пункты, расходящиеся с внутренней политикой. Требуется решение ORG_ADMIN.',
    });
  }

  if (actions.length === 0) {
    actions.push({
      id: 'ready-to-sign',
      title: 'Договор готов к подписанию',
      description:
        'Существенных рисков не выявлено. Скачайте отчёт и передайте его заинтересованным сторонам.',
    });
  }

  return actions;
}

export function NextActions({ results }: NextActionsProps): JSX.Element {
  const actions = buildActions(results);
  return (
    <section
      aria-label="Следующие шаги"
      data-testid="next-actions"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Следующие шаги
        </h2>
        <p className="text-xs text-fg-muted">Что сделать на основании результатов проверки</p>
      </header>

      <ol className="flex flex-col gap-2" data-testid="next-actions-list">
        {actions.map((action, idx) => (
          <li
            key={action.id}
            data-testid={`next-action-${action.id}`}
            className="flex gap-3 rounded-md border border-border bg-bg-muted p-3"
          >
            <span
              aria-hidden="true"
              className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-brand-500 text-xs font-semibold text-white"
            >
              {idx + 1}
            </span>
            <div className="flex flex-col gap-1">
              <p className="text-sm font-medium text-fg">{action.title}</p>
              <p className="text-xs text-fg-muted">{action.description}</p>
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}
