// ReportsShared — блок «Отчёты и ссылки» (экран 8 Figma, §17.4).
// Для v1 FE-TASK-045 — информативный плейсхолдер с ссылкой на /reports.
// Реальные отчёты договора (PDF/DOCX экспорт, shared links) подключаются
// через features/export-download + features/share-link на ResultPage
// (FE-TASK-046) — там есть версия с готовым анализом.
import { Link } from 'react-router-dom';

import { buttonVariants } from '@/shared/ui';

export function ReportsShared(): JSX.Element {
  return (
    <section
      aria-label="Отчёты и общие ссылки"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Отчёты и общие ссылки
        </h2>
        <p className="mt-1 text-xs text-fg-muted">
          Экспортируйте разбор договора в PDF/DOCX или поделитесь ссылкой.
        </p>
      </header>
      <p className="text-sm text-fg-muted">
        Отчёты и ссылки доступны на странице результата текущей версии. Для общего управления
        перейдите в раздел отчётов.
      </p>
      <Link
        to="/reports"
        className={`${buttonVariants({ variant: 'secondary', size: 'md' })} self-start`}
      >
        Перейти в «Отчёты»
      </Link>
    </section>
  );
}
