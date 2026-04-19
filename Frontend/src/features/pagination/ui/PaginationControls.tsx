import { useMemo } from 'react';

import { cn } from '@/shared/lib/cn';
import { PageSizeSelect, Pagination } from '@/shared/ui/pagination';

import { PAGE_SIZE_OPTIONS } from '../model/constants';

export interface PaginationControlsProps {
  page: number;
  size: number;
  total: number;
  onPageChange: (page: number) => void;
  onSizeChange?: (size: number) => void;
  pageSizeOptions?: readonly number[];
  /** true — показать скелет-строку вместо номеров страниц (во время первого fetch'а). */
  isLoading?: boolean;
  /** Блокирует элементы (например, во время fetch с placeholderData). */
  isFetching?: boolean;
  className?: string;
  /** Локализованный формат «A–B из N записей». */
  renderRange?: (args: {
    from: number;
    to: number;
    total: number;
    page: number;
    size: number;
  }) => string;
  /** aria-label на корневом <div>. */
  ariaLabel?: string;
}

function defaultRenderRange({ from, to, total }: { from: number; to: number; total: number }) {
  return `${from.toLocaleString('ru-RU')}–${to.toLocaleString('ru-RU')} из ${total.toLocaleString('ru-RU')}`;
}

export function PaginationControls({
  page,
  size,
  total,
  onPageChange,
  onSizeChange,
  pageSizeOptions = PAGE_SIZE_OPTIONS,
  isLoading = false,
  isFetching = false,
  className,
  renderRange = defaultRenderRange,
  ariaLabel = 'Пагинация',
}: PaginationControlsProps) {
  const totalPages = useMemo(
    () => (total > 0 && size > 0 ? Math.ceil(total / size) : 0),
    [total, size],
  );
  // Клэмп защищает от рассинхрона (URL хранит page=10, но total сократился до 5 страниц).
  const safePage = totalPages > 0 ? Math.min(Math.max(1, Math.floor(page)), totalPages) : 1;
  const from = total > 0 ? (safePage - 1) * size + 1 : 0;
  const to = Math.min(safePage * size, total);

  return (
    <div
      className={cn(
        'flex flex-wrap items-center justify-between gap-3 py-2 text-sm text-fg',
        className,
      )}
      aria-label={ariaLabel}
    >
      <div className="text-fg-muted" aria-live="polite">
        {isLoading ? (
          <span className="inline-block h-4 w-40 animate-pulse rounded bg-bg-muted" />
        ) : total > 0 ? (
          renderRange({ from, to, total, page: safePage, size })
        ) : (
          'Записей нет'
        )}
      </div>
      <div className="flex items-center gap-3">
        {onSizeChange ? (
          <PageSizeSelect
            value={size}
            options={pageSizeOptions}
            onChange={onSizeChange}
            disabled={isLoading}
          />
        ) : null}
        <Pagination
          page={safePage}
          totalPages={totalPages}
          onPageChange={onPageChange}
          disabled={isLoading || isFetching}
        />
      </div>
    </div>
  );
}
