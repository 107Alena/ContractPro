// LandingHeader — Figma node 6:2. Публичный header для Landing-страницы:
// логотип + nav-ссылки на якоря секций + 2 CTA (Войти secondary, Попробовать
// бесплатно primary). Не путать с widgets/topbar (тот для AppLayout
// аутентифицированных пользователей).
import { Link } from 'react-router-dom';

import { cn } from '@/shared/lib/cn';
import { buttonVariants } from '@/shared/ui';

interface NavLink {
  label: string;
  href: string;
}

const NAV_LINKS: ReadonlyArray<NavLink> = [
  { label: 'Возможности', href: '#features' },
  { label: 'Как это работает', href: '#how-it-works' },
  { label: 'Для кого', href: '#for-whom' },
  { label: 'Тарифы', href: '#pricing' },
  { label: 'FAQ', href: '#faq' },
];

export function LandingHeader(): JSX.Element {
  return (
    <header className="sticky top-0 z-10 border-b border-border-subtle bg-bg/95 backdrop-blur supports-[backdrop-filter]:bg-bg/80">
      <div className="mx-auto flex w-full max-w-[1280px] items-center justify-between gap-6 px-4 py-4 sm:px-6 lg:px-20">
        <Link to="/" className="flex items-center gap-2" aria-label="ContractPro главная">
          <span aria-hidden="true" className="size-7 rounded-[7px] bg-brand-500" />
          <span className="text-20 font-bold text-fg">ContractPro</span>
        </Link>

        <nav aria-label="Главное меню" className="hidden lg:block">
          <ul className="flex items-center gap-8 text-15 font-medium text-fg-muted">
            {NAV_LINKS.map((link) => (
              <li key={link.href}>
                <a className="transition-colors hover:text-fg" href={link.href}>
                  {link.label}
                </a>
              </li>
            ))}
          </ul>
        </nav>

        <div className="flex items-center gap-3">
          <Link
            to="/login"
            className={cn(
              buttonVariants({ variant: 'secondary', size: 'md' }),
              'hidden sm:inline-flex',
            )}
          >
            Войти
          </Link>
          <Link to="/login" className={buttonVariants({ variant: 'primary', size: 'md' })}>
            Попробовать бесплатно
          </Link>
        </div>
      </div>
    </header>
  );
}
