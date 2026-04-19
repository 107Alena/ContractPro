// PDFNavigator — stub-виджет для экрана «Карточка документа» (FE-TASK-045,
// §6.3 «chunks/pdf-preview»). В v1 реального превью PDF нет: pdfjs-dist
// (~500 КБ) не устанавливается в рамках FE-TASK-045. Стаб нужен, чтобы:
//   1) заявлен отдельный Vite-chunk `chunks/pdf-preview` (см. vite.config
//      manualChunks); size-limit бюджетирует его как granular-chunk.
//   2) page может динамически импортировать виджет через React.lazy, что
//      подтверждает AC «PDF preview lazy-загружается только при тумблере».
//
// Когда pdfjs-dist появится (см. FE-TASK-044/046 или v1.0.1), содержимое
// компонента переедет на реальный pdf viewer без изменения формы chunk'а
// и без переработки page-композиции.
import { Button } from '@/shared/ui';

export interface PDFNavigatorProps {
  versionId: string;
  sourceFileName?: string | undefined;
  onClose?: () => void;
}

// default-export для удобства React.lazy (без промежуточного { default: mod.X }).
export default function PDFNavigator({
  versionId,
  sourceFileName,
  onClose,
}: PDFNavigatorProps): JSX.Element {
  return (
    <section
      aria-label="Превью PDF"
      data-testid="pdf-navigator"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
            Превью PDF
          </h2>
          <p className="mt-1 text-xs text-fg-muted">
            Версия: <code>{versionId}</code>
            {sourceFileName ? ` · ${sourceFileName}` : null}
          </p>
        </div>
        {onClose ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onClose}
            data-testid="pdf-navigator-close"
          >
            Скрыть
          </Button>
        ) : null}
      </header>

      <div className="flex min-h-[240px] flex-col items-center justify-center gap-2 rounded-md border border-dashed border-border bg-bg-muted p-6 text-center">
        <p className="text-sm font-medium text-fg">PDF-просмотр станет доступен после интеграции</p>
        <p className="max-w-md text-xs text-fg-muted">
          Библиотека pdfjs-dist подгружается отдельным чанком (chunks/pdf-preview) и будет
          подключена в последующей итерации. Сейчас отображается заглушка — файлы и результаты
          анализа уже доступны в остальных блоках карточки.
        </p>
      </div>
    </section>
  );
}
