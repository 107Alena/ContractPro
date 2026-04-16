import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type HTMLAttributes, type KeyboardEvent, type MouseEvent } from 'react';

import { cn } from '@/shared/lib/cn';

const chipVariants = cva(
  [
    'inline-flex items-center gap-1.5 whitespace-nowrap',
    'rounded-md border border-border bg-bg-muted px-3 py-1',
    'text-sm font-medium text-fg',
    'transition-colors duration-150',
  ],
  {
    variants: {
      selected: {
        true: 'border-brand-500 bg-brand-50 text-brand-600',
        false: '',
      },
      interactive: {
        true: 'cursor-pointer hover:border-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
        false: '',
      },
    },
    defaultVariants: { selected: false, interactive: false },
  },
);

type ChipVariantProps = VariantProps<typeof chipVariants>;

export interface ChipProps
  extends Omit<HTMLAttributes<HTMLSpanElement>, 'onClick'>, ChipVariantProps {
  onRemove?: () => void;
  removeLabel?: string;
  onClick?: (e: MouseEvent<HTMLSpanElement>) => void;
}

export const Chip = forwardRef<HTMLSpanElement, ChipProps>(function Chip(
  {
    className,
    selected,
    interactive,
    onRemove,
    removeLabel,
    onClick,
    onKeyDown,
    children,
    ...rest
  },
  ref,
) {
  const hasInteraction = interactive || Boolean(onClick);

  function handleKeyDown(e: KeyboardEvent<HTMLSpanElement>) {
    onKeyDown?.(e);
    if (e.defaultPrevented) return;
    if (onRemove && (e.key === 'Backspace' || e.key === 'Delete')) {
      e.preventDefault();
      onRemove();
    }
  }

  return (
    <span
      ref={ref}
      className={cn(chipVariants({ selected, interactive: hasInteraction }), className)}
      role={hasInteraction ? 'button' : undefined}
      tabIndex={hasInteraction ? 0 : undefined}
      onClick={onClick}
      onKeyDown={handleKeyDown}
      {...rest}
    >
      <span>{children}</span>
      {onRemove ? (
        <button
          type="button"
          aria-label={removeLabel ?? 'Удалить'}
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          className={cn(
            'inline-flex h-4 w-4 items-center justify-center rounded-sm',
            'text-fg-muted hover:text-fg hover:bg-border/50',
            'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
          )}
        >
          <svg
            aria-hidden="true"
            focusable="false"
            viewBox="0 0 16 16"
            width="12"
            height="12"
            fill="none"
          >
            <path
              d="M4 4l8 8M12 4l-8 8"
              stroke="currentColor"
              strokeWidth="1.75"
              strokeLinecap="round"
            />
          </svg>
        </button>
      ) : null}
    </span>
  );
});

export { chipVariants };
