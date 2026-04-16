// <Can> — компонентный guard секций/кнопок по permission-ключу (§5.6 Pattern B).
// Рендерит children при наличии прав, иначе — `fallback` (default null).
// Архитектура: Frontend/architecture/high-architecture.md §5.5 / §5.6 / §20.3.
import type { ReactNode } from 'react';

import { type Permission, useCan } from './rbac';

export interface CanProps {
  I: Permission;
  children: ReactNode;
  fallback?: ReactNode;
}

export function Can({ I, children, fallback = null }: CanProps): ReactNode {
  const allowed = useCan(I);
  return allowed ? children : fallback;
}
