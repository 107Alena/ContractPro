// ReportsShared — блок «Отчёты и shared results» (Figma 306:2 → 315:20).
// Honest-плейсхолдер: реальные отчёты договора (PDF/DOCX экспорт, shared links)
// живут на странице результата текущей версии (features/export-download +
// features/share-link, FE-TASK-046) — там есть готовый анализ. Здесь —
// приглашение перейти. Card flat-border treatment под Figma 306:2.
import { Link } from 'react-router-dom';

import { buttonVariants, Card } from '@/shared/ui';

export function ReportsShared(): JSX.Element {
  return (
    <Card
      as="section"
      aria-label="Отчёты и общие ссылки"
      radius="xl"
      className="flex flex-col gap-3 border border-border-subtle px-7 py-6 shadow-none"
    >
      <h2 className="text-18 font-semibold text-fg">Отчёты и shared results</h2>
      <p className="text-14 leading-5 text-fg-muted">
        Отчёты и ссылки доступны на странице результата текущей версии. Для общего управления
        перейдите в раздел отчётов.
      </p>
      <Link
        to="/reports"
        className={`${buttonVariants({ variant: 'secondary', size: 'md' })} self-start`}
      >
        Перейти в «Отчёты»
      </Link>
    </Card>
  );
}
