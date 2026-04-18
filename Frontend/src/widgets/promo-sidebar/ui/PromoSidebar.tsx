// PromoSidebar (FE-TASK-029) — левая колонка на Auth Page (§17.4).
//
// Desktop: закреплённая колонка 40-45% ширины вьюпорта с брендом ContractPro,
// кратким оффером и ключевыми преимуществами. Mobile (<md): скрывается —
// Figma mobile-макет показывает только форму (см. §8.3 responsive).
//
// Статический контент (без i18n до FE-TASK-030). Визуальный тон — бренд-оранж
// (`bg-brand-500`) на тёмном фоне, контрастный белый текст — соответствует
// §8.2 tokens (brand-500 = #F55E12).
import type { ReactNode } from 'react';

export interface PromoSidebarProps {
  className?: string;
}

interface HighlightItem {
  title: string;
  description: string;
}

const HIGHLIGHTS: HighlightItem[] = [
  {
    title: 'Проверка за 1–2 минуты',
    description: 'Юридический анализ договоров по ГК РФ без ожидания юристов.',
  },
  {
    title: 'Карта рисков и рекомендации',
    description: 'Подсветим проблемные пункты и предложим корректные формулировки.',
  },
  {
    title: 'Экспорт и общий доступ',
    description: 'Выгрузка отчёта в PDF/DOCX, ссылки на результат для команды.',
  },
];

function ShieldIcon(): JSX.Element {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 20 20"
      className="h-5 w-5 shrink-0"
      fill="currentColor"
    >
      <path
        d="M10 1.5 3.5 4v5.1c0 4.1 2.7 7.8 6.5 9.4 3.8-1.6 6.5-5.3 6.5-9.4V4L10 1.5Zm-.9 11.6-3.1-3 1.3-1.3 1.8 1.7 4.1-4.1 1.3 1.3-5.4 5.4Z"
      />
    </svg>
  );
}

export function PromoSidebar({ className }: PromoSidebarProps): JSX.Element {
  return (
    <aside
      aria-label="ContractPro — проверка договоров"
      className={[
        'hidden md:flex',
        'flex-col justify-between gap-10 p-10 xl:p-14',
        'bg-brand-500 text-white',
        className ?? '',
      ].join(' ')}
      data-testid="promo-sidebar"
    >
      <header className="flex flex-col gap-3">
        <BrandMark />
        <h2 className="text-3xl font-semibold leading-tight xl:text-4xl">
          ИИ-проверка договоров для&nbsp;юристов и&nbsp;бизнеса
        </h2>
        <p className="text-base text-white/85">
          Анализ по ГК РФ, риски, мандатные условия и&nbsp;рекомендации —
          в&nbsp;одном окне.
        </p>
      </header>

      <ul className="flex flex-col gap-5">
        {HIGHLIGHTS.map((item) => (
          <HighlightRow key={item.title} icon={<ShieldIcon />}>
            <span className="block text-base font-semibold">{item.title}</span>
            <span className="block text-sm text-white/80">{item.description}</span>
          </HighlightRow>
        ))}
      </ul>

      <footer className="flex flex-col gap-1 text-sm text-white/75">
        <span>Безопасное хранение документов на серверах в РФ.</span>
        <span>© {new Date().getFullYear()} ContractPro</span>
      </footer>
    </aside>
  );
}

function BrandMark(): JSX.Element {
  return (
    <div className="flex items-center gap-2 text-lg font-semibold">
      <span
        aria-hidden="true"
        className="inline-flex h-8 w-8 items-center justify-center rounded-md bg-white/15 text-white"
      >
        CP
      </span>
      ContractPro
    </div>
  );
}

function HighlightRow({
  icon,
  children,
}: {
  icon: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <li className="flex items-start gap-3">
      <span className="mt-0.5 text-white">{icon}</span>
      <div className="flex flex-col">{children}</div>
    </li>
  );
}
