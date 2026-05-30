// QuickStart — «Быстрый старт» на dashboard (Figma 84:2 → 89:36).
// Список из 7 быстрых навигационных действий. RBAC: скрывается на уровне page
// через <Can I="contract.upload"/>, сам виджет визуально не ограничен.
import { type ComponentType, type SVGProps } from 'react';
import { Link } from 'react-router-dom';

import { Card } from '@/shared/ui';

import {
  CompareIcon,
  DocsIcon,
  DownloadIcon,
  PasteIcon,
  ScanIcon,
  ShareIcon,
  UploadIcon,
} from '../icons';

export interface QuickStartProps {
  className?: string;
}

type IconComponent = ComponentType<SVGProps<SVGSVGElement>>;

const ACTIONS: ReadonlyArray<{ label: string; to: string; Icon: IconComponent }> = [
  { label: 'Новая проверка', to: '/contracts/new', Icon: ScanIcon },
  { label: 'Загрузить договор', to: '/contracts/new', Icon: UploadIcon },
  { label: 'Вставить текст', to: '/contracts/new', Icon: PasteIcon },
  { label: 'Сравнить версии', to: '/contracts', Icon: CompareIcon },
  { label: 'Открыть документы', to: '/contracts', Icon: DocsIcon },
  { label: 'Скачать последний отчёт', to: '/reports', Icon: DownloadIcon },
  { label: 'Поделиться ссылкой', to: '/reports', Icon: ShareIcon },
];

export function QuickStart({ className }: QuickStartProps): JSX.Element {
  return (
    <Card
      aria-label="Быстрый старт"
      className={['flex flex-col gap-3 p-5', className ?? ''].filter(Boolean).join(' ')}
    >
      <h2 className="text-15 font-semibold text-fg">Быстрый старт</h2>
      <ul className="flex flex-col gap-1">
        {ACTIONS.map(({ label, to, Icon }) => (
          <li key={label}>
            <Link
              to={to}
              className="flex items-center gap-2.5 rounded-[8px] px-2.5 py-[7px] text-13 font-medium text-fg-muted transition-colors hover:bg-bg-muted focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
            >
              <Icon className="size-[18px] shrink-0 text-fg-subtle" />
              {label}
            </Link>
          </li>
        ))}
      </ul>
    </Card>
  );
}
