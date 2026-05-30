// LandingFooter — Figma node 24:11. Тёмный footer с 4 колонками
// (Brand + Продукт + Компания + Правовая информация) и нижней строкой
// copyright + авторы.
interface FooterColumn {
  title: string;
  links: ReadonlyArray<{ label: string; href: string }>;
}

const COLUMNS: ReadonlyArray<FooterColumn> = [
  {
    title: 'Продукт',
    links: [
      { label: 'Возможности', href: '#features' },
      { label: 'Тарифы', href: '#pricing' },
      { label: 'Как это работает', href: '#how-it-works' },
      { label: 'FAQ', href: '#faq' },
    ],
  },
  {
    title: 'Компания',
    links: [
      { label: 'О нас', href: '#' },
      { label: 'Блог', href: '#' },
      { label: 'Карьера', href: '#' },
      { label: 'Контакты', href: '#' },
    ],
  },
  {
    title: 'Правовая информация',
    links: [
      { label: 'Политика конфиденциальности', href: '#' },
      { label: 'Пользовательское соглашение', href: '#' },
      { label: 'Оферта', href: '#' },
    ],
  },
];

export function LandingFooter(): JSX.Element {
  return (
    <footer className="bg-fg px-4 py-12 sm:px-6 lg:px-20 lg:py-12">
      <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-8">
        <div className="grid grid-cols-1 gap-10 sm:grid-cols-2 lg:grid-cols-[320px_1fr_1fr_1fr] lg:gap-20">
          <div className="flex flex-col gap-3">
            <p className="text-20 font-bold text-white">ContractPro</p>
            <p className="text-14 leading-[22px] text-fg-disabled">
              AI-платформа для проверки и анализа договоров в юрисдикции РФ
            </p>
          </div>
          {COLUMNS.map((col) => (
            <div key={col.title} className="flex flex-col gap-3.5 text-14">
              <p className="font-semibold text-fg-disabled">{col.title}</p>
              <ul className="flex flex-col gap-3.5">
                {col.links.map((link) => (
                  <li key={link.label}>
                    <a className="text-border transition-colors hover:text-white" href={link.href}>
                      {link.label}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="h-px w-full bg-white/10" />

        <div className="flex flex-col items-start justify-between gap-2 text-13 sm:flex-row sm:items-center">
          <p className="text-fg-subtle">© 2026 ContractPro. Все права защищены.</p>
          <p className="text-fg-disabled">@alenabaranovaa @AndreyBlyudin</p>
        </div>
      </div>
    </footer>
  );
}
