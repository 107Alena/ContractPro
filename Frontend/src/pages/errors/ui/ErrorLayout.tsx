import type { ReactNode } from 'react';

import { cn } from '@/shared/lib/cn';

export interface ErrorLayoutProps {
  code?: string;
  title: string;
  description?: string;
  children?: ReactNode;
  className?: string;
}

/**
 * Общий layout для страниц 403/404/500/offline и route-error fallback.
 * §9.2, §17.1 high-architecture.
 */
export function ErrorLayout({
  code,
  title,
  description,
  children,
  className,
}: ErrorLayoutProps): JSX.Element {
  return (
    <main
      role="main"
      className={cn(
        'mx-auto flex min-h-[60vh] max-w-2xl flex-col items-center justify-center gap-4 px-6 py-16 text-center',
        className,
      )}
    >
      {code ? (
        <p className="text-sm font-semibold uppercase tracking-wider text-fg-muted">{code}</p>
      ) : null}
      <h1 className="text-3xl font-semibold text-fg">{title}</h1>
      {description ? <p className="max-w-lg text-base text-fg-muted">{description}</p> : null}
      {children ? <div className="mt-2 flex flex-wrap justify-center gap-3">{children}</div> : null}
    </main>
  );
}
