import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type HTMLAttributes, type KeyboardEvent, type MouseEvent } from 'react';

import { cn } from '@/shared/lib/cn';

// Figma-aligned: nodes 177:3 (FilterChip selected — solid brand) и 177:5
// (FilterChip unselected — white + subtle border) — Comparison page.
// Selected = solid brand-500 + white text (не светло-оранжевый, как было).
const chipVariants = cva(
  [
    'inline-flex items-center gap-1.5 whitespace-nowrap',
    // pill 20px + figma padding 14/7 (py-[7px] inline — нет 7-pt в token-scale)
    'rounded-pill border px-3.5 py-[7px]',
    'text-13 font-medium leading-4',
    'transition-colors duration-150',
  ],
  {
    variants: {
      selected: {
        true: 'border-transparent bg-brand-500 text-white',
        false: 'border-border-subtle bg-bg text-fg-muted',
      },
      interactive: {
        true: 'cursor-pointer focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
        false: '',
      },
    },
    compoundVariants: [
      // Hover: selected → darker brand; unselected → выделить border брендом.
      { interactive: true, selected: true, class: 'hover:bg-brand-600' },
      { interactive: true, selected: false, class: 'hover:border-brand-500' },
    ],
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
