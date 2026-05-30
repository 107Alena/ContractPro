// WelcomeBlock — приветственный блок dashboard (Figma 84:2 → 87:3).
//
// Приветствие по имени (useMe), три CTA и микрокопия. Все три CTA ведут на
// /contracts/new (загрузка/вставка — варианты одного flow). Микрокопия отражает
// РЕАЛЬНЫЕ ограничения продукта (PDF, до 20 МБ — FileDropZone), а не Figma-текст
// «DOC, DOCX, PDF · 50 МБ», который противоречит v1 (PDF-only, 20 МБ).
import { Link } from 'react-router-dom';

import { type UserProfile } from '@/entities/user';
import { buttonVariants, Card } from '@/shared/ui';

import { CheckScanIcon, PasteIcon, UploadIcon } from '../icons';

export interface WelcomeBlockProps {
  user?: UserProfile | undefined;
}

function firstName(name?: string): string {
  if (!name) return '';
  return name.trim().split(/\s+/)[0] ?? '';
}

export function WelcomeBlock({ user }: WelcomeBlockProps): JSX.Element {
  const name = firstName(user?.name);

  return (
    <Card radius="xl" aria-label="Приветствие" className="flex flex-col gap-4 px-8 py-7">
      <div className="flex flex-col gap-1.5">
        <h1 className="text-24 font-bold text-fg">
          {name ? `Добро пожаловать, ${name}` : 'Добро пожаловать'}
        </h1>
        <p className="text-15 text-fg-muted">
          Начните новую проверку договора или откройте последний результат
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Link to="/contracts/new" className={buttonVariants({ variant: 'primary', size: 'md' })}>
          <CheckScanIcon className="size-[18px]" />
          Новая проверка
        </Link>
        <Link to="/contracts/new" className={buttonVariants({ variant: 'secondary', size: 'md' })}>
          <UploadIcon className="size-[18px]" />
          Загрузить договор
        </Link>
        <Link to="/contracts/new" className={buttonVariants({ variant: 'secondary', size: 'md' })}>
          <PasteIcon className="size-[18px]" />
          Вставить текст
        </Link>
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-12 text-fg-disabled">
        <span>Формат: PDF · до 20 МБ</span>
        <span>🔒 Документы защищены и не передаются третьим лицам</span>
      </div>
    </Card>
  );
}
