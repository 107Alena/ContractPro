// AppBreadcrumbs — route-aware обёртка над shared/ui/breadcrumbs.
// Источник данных: useMatches() React Router + route.handle.crumb (§6.4).
//
// Навигация через RR <Link>: обычный <a href> из shared/ui/breadcrumbs
// вызвал бы full page reload и сбросил бы in-memory access-token (§5.2).
// Поэтому каждый intermediate crumb оборачиваем в BreadcrumbsLink asChild + Link.
//
// useMatches() в react-router-dom 6.22 привязан к data-router (RouterProvider).
// В тестах роутера (src/app/router/router.test.tsx) используется MemoryRouter
// + useRoutes (declarative API) — комментарий в том файле объясняет, что
// data-router + RouterProvider несовместим с Node 20 + undici. Чтобы
// Breadcrumbs не ломали такие тесты — детектируем data-router через
// UNSAFE_DataRouterStateContext и в declarative-режиме не вызываем useMatches.
// TODO(FE-TASK-033-followup): удалить UNSAFE_* guard после миграции
// router.test.tsx на createMemoryRouter + RouterProvider (Node 22 уже снял
// ограничение undici с AbortSignal — см. прогресс в Frontend/progress.md).
import { useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, UNSAFE_DataRouterStateContext, useMatches } from 'react-router-dom';

import { cn } from '@/shared/lib/cn';
import {
  type BreadcrumbItem,
  BreadcrumbsEllipsis,
  BreadcrumbsItem,
  BreadcrumbsLink,
  BreadcrumbsList,
  BreadcrumbsPage,
  BreadcrumbsRoot,
  BreadcrumbsSeparator,
} from '@/shared/ui/breadcrumbs';

import { resolveCrumbs } from './resolve-crumbs';

export interface AppBreadcrumbsProps {
  /** aria-label корневого <nav>. По умолчанию — из i18n (`common:breadcrumbs.label`). */
  label?: string;
  /** Свернуть середину в «…» если items.length > maxItems. */
  maxItems?: number;
  /** className передаётся на <nav>. */
  className?: string;
  /**
   * Тест-оверрайд: явный список items вместо useMatches() из контекста. Для
   * Storybook и изолированных unit-тестов, где MemoryRouter не удобно
   * собирать. В приложении оставляем undefined.
   */
  items?: ReadonlyArray<BreadcrumbItem>;
}

const DEFAULT_MAX_ITEMS = 5;
const ITEMS_BEFORE_COLLAPSE = 1;
const ITEMS_AFTER_COLLAPSE = 1;

interface BreadcrumbsViewProps {
  items: ReadonlyArray<BreadcrumbItem>;
  label: string;
  maxItems: number;
  className?: string;
}

function BreadcrumbsView({
  items,
  label,
  maxItems,
  className,
}: BreadcrumbsViewProps): JSX.Element | null {
  // Нет breadcrumb-совместимых matches — не рендерим <nav>. Причина: в раннем
  // route-lifecycle (до завершения lazy-лоадинга страницы) useMatches() может
  // вернуть только pathless layout без handle.crumb. Пустой <nav> с aria-label
  // в assistive-tech озвучивается как «Хлебные крошки, без элементов» — хуже,
  // чем отсутствие ориентира.
  if (items.length === 0) return null;

  const shouldCollapse =
    items.length > maxItems && items.length > ITEMS_BEFORE_COLLAPSE + ITEMS_AFTER_COLLAPSE;

  const visibleItems: ReadonlyArray<BreadcrumbItem | { id: '__ellipsis__'; label: string }> =
    shouldCollapse
      ? [
          ...items.slice(0, ITEMS_BEFORE_COLLAPSE),
          { id: '__ellipsis__' as const, label: '…' },
          ...items.slice(items.length - ITEMS_AFTER_COLLAPSE),
        ]
      : [...items];

  const lastIdx = visibleItems.length - 1;

  return (
    <BreadcrumbsRoot
      label={label}
      className={cn('px-6 py-3 border-b border-border bg-bg', className)}
      data-testid="app-breadcrumbs"
    >
      <BreadcrumbsList>
        {visibleItems.map((item, idx) => {
          const isLast = idx === lastIdx;
          const isEllipsis = item.id === '__ellipsis__';
          const isCurrent = !isEllipsis && ('current' in item ? item.current === true : isLast);
          const href = !isEllipsis && 'href' in item ? item.href : undefined;

          return (
            <BreadcrumbsItem key={item.id ?? `bc-${idx}`}>
              {isEllipsis ? (
                <BreadcrumbsEllipsis />
              ) : isCurrent || !href ? (
                <BreadcrumbsPage aria-current={isCurrent ? 'page' : undefined}>
                  {item.label}
                </BreadcrumbsPage>
              ) : (
                <BreadcrumbsLink asChild>
                  <Link to={href}>{item.label}</Link>
                </BreadcrumbsLink>
              )}
              {isLast ? null : <BreadcrumbsSeparator />}
            </BreadcrumbsItem>
          );
        })}
      </BreadcrumbsList>
    </BreadcrumbsRoot>
  );
}

function DataRouterBreadcrumbs(
  props: Omit<BreadcrumbsViewProps, 'items'> & { itemsOverride?: ReadonlyArray<BreadcrumbItem> },
): JSX.Element | null {
  const matches = useMatches();
  const items = props.itemsOverride ?? resolveCrumbs(matches);
  return <BreadcrumbsView {...props} items={items} />;
}

export function AppBreadcrumbs({
  label,
  maxItems = DEFAULT_MAX_ITEMS,
  className,
  items: itemsOverride,
}: AppBreadcrumbsProps = {}): JSX.Element | null {
  const { t } = useTranslation(['common']);
  const resolvedLabel = label ?? t('common:breadcrumbs.label');
  const dataRouterState = useContext(UNSAFE_DataRouterStateContext);
  const viewProps = {
    label: resolvedLabel,
    maxItems,
    ...(className !== undefined ? { className } : {}),
  };

  // Items override: рендерим без обращения к data-router.
  if (itemsOverride !== undefined) {
    return <BreadcrumbsView {...viewProps} items={itemsOverride} />;
  }

  // Declarative MemoryRouter (useMatches() бы бросил invariant) — no-op.
  if (dataRouterState === null) return null;

  return <DataRouterBreadcrumbs {...viewProps} />;
}

export { AppBreadcrumbs as Breadcrumbs };
