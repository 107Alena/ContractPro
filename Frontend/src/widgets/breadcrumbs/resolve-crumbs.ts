// resolveCrumbs — чистый helper трансформации React Router matches в
// BreadcrumbItem[] для shared/ui Breadcrumbs (§6.4 high-architecture).
//
// Фильтрация: берутся только matches, у которых handle.crumb определён
// (строка или (match) => string). Остальные matches (например, pathless layout
// route с AppLayout — не несёт собственного breadcrumb) пропускаются.
//
// Последний match после фильтрации помечается current=true и не получает href
// — shared/ui/breadcrumbs рендерит его как BreadcrumbsPage[aria-current=page].
// Intermediate — current=false + href из match.pathname.
import type { UIMatch } from 'react-router-dom';

import type { BreadcrumbItem } from '@/shared/ui/breadcrumbs';

export type CrumbResolver = (match: UIMatch) => string;
export type CrumbValue = string | CrumbResolver;
export interface RouteHandleWithCrumb {
  crumb: CrumbValue;
}

function hasCrumb(match: UIMatch): match is UIMatch & { handle: RouteHandleWithCrumb } {
  const handle = match.handle;
  if (handle === null || typeof handle !== 'object') return false;
  const crumb = (handle as { crumb?: unknown }).crumb;
  return typeof crumb === 'string' || typeof crumb === 'function';
}

function resolveLabel(match: UIMatch, crumb: CrumbValue): string {
  return typeof crumb === 'function' ? crumb(match) : crumb;
}

export function resolveCrumbs(matches: readonly UIMatch[]): BreadcrumbItem[] {
  const withCrumb = matches.filter(hasCrumb);
  return withCrumb.map((match, idx): BreadcrumbItem => {
    const isLast = idx === withCrumb.length - 1;
    const label = resolveLabel(match, match.handle.crumb);
    return isLast
      ? { id: match.id, label, current: true }
      : { id: match.id, label, href: match.pathname, current: false };
  });
}
