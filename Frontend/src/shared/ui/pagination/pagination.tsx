import { cva, type VariantProps } from 'class-variance-authority';
import { type ChangeEvent, forwardRef, type HTMLAttributes, useMemo } from 'react';

import { cn } from '@/shared/lib/cn';
import { Button } from '@/shared/ui/button';

const paginationVariants = cva(['flex items-center gap-1 flex-wrap', 'text-sm text-fg'], {
  variants: {
    size: {
      sm: 'text-xs',
      md: 'text-sm',
    },
  },
  defaultVariants: { size: 'md' },
});

type PaginationVariantProps = VariantProps<typeof paginationVariants>;

export interface PaginationProps
  extends Omit<HTMLAttributes<HTMLElement>, 'onChange'>, PaginationVariantProps {
  /** 1-based текущая страница. */
  page: number;
  /** Общее количество страниц. Если 0 или 1 — рендерим пустой nav. */
  totalPages: number;
  /** Соседние страницы слева и справа (по умолчанию 1 = [… 4 5 6 …]). */
  siblingCount?: number;
  /** Количество «якорных» страниц с краёв (default 1). */
  boundaryCount?: number;
  onPageChange: (page: number) => void;
  /** aria-label на корневом <nav>. По умолчанию 'Пагинация'. */
  label?: string;
  /** Локализованные подписи кнопок. */
  labels?: {
    previous?: string;
    next?: string;
    pageAriaLabel?: (page: number) => string;
    currentPageAriaLabel?: (page: number) => string;
  };
  /** Показывать ли кнопки «Назад/Вперёд». По умолчанию true. */
  showPrevNext?: boolean;
  /** Блокирует все элементы (например, во время fetch). */
  disabled?: boolean;
}

type PageToken = number | 'ellipsis-left' | 'ellipsis-right';

function buildPages({
  page,
  totalPages,
  siblingCount,
  boundaryCount,
}: {
  page: number;
  totalPages: number;
  siblingCount: number;
  boundaryCount: number;
}): PageToken[] {
  const totalNumbers = boundaryCount * 2 + siblingCount * 2 + 3;
  if (totalPages <= totalNumbers) {
    return Array.from({ length: totalPages }, (_, i) => i + 1);
  }

  const startPages = Array.from({ length: boundaryCount }, (_, i) => i + 1);
  const endPages = Array.from(
    { length: boundaryCount },
    (_, i) => totalPages - boundaryCount + i + 1,
  );
  const siblingStart = Math.max(
    Math.min(page - siblingCount, totalPages - boundaryCount - siblingCount * 2 - 1),
    boundaryCount + 2,
  );
  const siblingEnd = Math.min(
    Math.max(page + siblingCount, boundaryCount + siblingCount * 2 + 2),
    endPages[0]! - 2,
  );

  const tokens: PageToken[] = [...startPages];
  if (siblingStart > boundaryCount + 2) {
    tokens.push('ellipsis-left');
  } else if (boundaryCount + 1 < totalPages - boundaryCount) {
    tokens.push(boundaryCount + 1);
  }
  for (let i = siblingStart; i <= siblingEnd; i += 1) tokens.push(i);
  if (siblingEnd < totalPages - boundaryCount - 1) {
    tokens.push('ellipsis-right');
  } else if (totalPages - boundaryCount > boundaryCount) {
    tokens.push(totalPages - boundaryCount);
  }
  for (const p of endPages) {
    if (!tokens.includes(p)) tokens.push(p);
  }
  return tokens;
}

const DEFAULT_LABELS = {
  previous: 'Назад',
  next: 'Вперёд',
  pageAriaLabel: (p: number) => `Страница ${p}`,
  currentPageAriaLabel: (p: number) => `Страница ${p}, текущая`,
} as const;

export const Pagination = forwardRef<HTMLElement, PaginationProps>(function Pagination(
  {
    page,
    totalPages,
    siblingCount = 1,
    boundaryCount = 1,
    onPageChange,
    className,
    label = 'Пагинация',
    labels,
    showPrevNext = true,
    disabled = false,
    size,
    ...rest
  },
  ref,
) {
  const safeTotal = Math.max(0, Math.floor(totalPages));
  const safePage = Math.min(Math.max(1, Math.floor(page)), Math.max(1, safeTotal));

  const tokens = useMemo(
    () =>
      buildPages({
        page: safePage,
        totalPages: safeTotal,
        siblingCount: Math.max(0, siblingCount),
        boundaryCount: Math.max(1, boundaryCount),
      }),
    [safePage, safeTotal, siblingCount, boundaryCount],
  );

  const l = { ...DEFAULT_LABELS, ...labels };
  const btnSize: 'sm' | 'md' = size === 'sm' ? 'sm' : 'md';

  if (safeTotal <= 1) {
    return (
      <nav
        ref={ref}
        aria-label={label}
        className={cn(paginationVariants({ size }), className)}
        {...rest}
      />
    );
  }

  return (
    <nav
      ref={ref}
      aria-label={label}
      className={cn(paginationVariants({ size }), className)}
      {...rest}
    >
      {showPrevNext ? (
        <Button
          type="button"
          variant="ghost"
          size={btnSize}
          disabled={disabled || safePage <= 1}
          onClick={() => onPageChange(safePage - 1)}
          aria-label={l.previous}
        >
          {l.previous}
        </Button>
      ) : null}
      <ul className="flex items-center gap-1">
        {tokens.map((t, idx) => {
          if (t === 'ellipsis-left' || t === 'ellipsis-right') {
            return (
              <li key={`${t}-${idx}`} aria-hidden="true" className="px-2 text-fg-muted">
                …
              </li>
            );
          }
          const isCurrent = t === safePage;
          return (
            <li key={t}>
              <Button
                type="button"
                variant={isCurrent ? 'primary' : 'ghost'}
                size={btnSize}
                disabled={disabled}
                aria-current={isCurrent ? 'page' : undefined}
                aria-label={isCurrent ? l.currentPageAriaLabel(t) : l.pageAriaLabel(t)}
                onClick={() => {
                  if (!isCurrent) onPageChange(t);
                }}
              >
                {t}
              </Button>
            </li>
          );
        })}
      </ul>
      {showPrevNext ? (
        <Button
          type="button"
          variant="ghost"
          size={btnSize}
          disabled={disabled || safePage >= safeTotal}
          onClick={() => onPageChange(safePage + 1)}
          aria-label={l.next}
        >
          {l.next}
        </Button>
      ) : null}
    </nav>
  );
});

export interface PageSizeSelectProps extends Omit<HTMLAttributes<HTMLDivElement>, 'onChange'> {
  value: number;
  options: readonly number[];
  onChange: (value: number) => void;
  label?: string;
  disabled?: boolean;
  id?: string;
}

export function PageSizeSelect({
  value,
  options,
  onChange,
  label = 'На странице',
  disabled = false,
  id,
  className,
  ...rest
}: PageSizeSelectProps) {
  const selectId = id ?? 'page-size-select';
  function handleChange(e: ChangeEvent<HTMLSelectElement>) {
    const n = Number(e.target.value);
    if (Number.isFinite(n) && n > 0) onChange(n);
  }
  return (
    <div className={cn('flex items-center gap-2 text-sm text-fg', className)} {...rest}>
      <label htmlFor={selectId} className="text-fg-muted">
        {label}
      </label>
      <select
        id={selectId}
        value={value}
        disabled={disabled}
        onChange={handleChange}
        className={cn(
          'h-8 rounded-md border border-border bg-bg px-2 text-sm text-fg',
          'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-0',
          'disabled:cursor-not-allowed disabled:bg-bg-muted disabled:text-fg-muted',
        )}
      >
        {options.map((opt) => (
          <option key={opt} value={opt}>
            {opt}
          </option>
        ))}
      </select>
    </div>
  );
}

export { paginationVariants };
