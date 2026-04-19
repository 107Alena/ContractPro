import { cva, type VariantProps } from 'class-variance-authority';
import {
  forwardRef,
  type HTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
  useCallback,
  useRef,
} from 'react';

import { cn } from '@/shared/lib/cn';

const segmentedControlVariants = cva(
  [
    'inline-flex items-center gap-0.5 rounded-md border border-border bg-bg-muted p-1',
    'focus-within:ring focus-within:ring-offset-1',
    'aria-disabled:opacity-60',
  ],
  {
    variants: {
      size: {
        sm: 'h-8',
        md: 'h-10',
      },
      fullWidth: {
        true: 'w-full',
        false: '',
      },
    },
    defaultVariants: { size: 'md', fullWidth: false },
  },
);

const segmentedItemVariants = cva(
  [
    'inline-flex items-center justify-center gap-1.5 rounded-sm px-3 font-medium',
    'select-none whitespace-nowrap',
    'transition-colors duration-150',
    'focus-visible:outline-none',
    'cursor-pointer',
    'aria-checked:bg-bg aria-checked:text-fg aria-checked:shadow-sm',
    'aria-[checked=false]:text-fg-muted hover:aria-[checked=false]:text-fg',
    'aria-disabled:pointer-events-none aria-disabled:cursor-not-allowed aria-disabled:opacity-50',
  ],
  {
    variants: {
      size: {
        sm: 'h-6 text-xs',
        md: 'h-8 text-sm',
      },
      fullWidth: {
        true: 'flex-1',
        false: '',
      },
    },
    defaultVariants: { size: 'md', fullWidth: false },
  },
);

export interface SegmentedControlOption<TValue extends string = string> {
  value: TValue;
  label: ReactNode;
  icon?: ReactNode;
  disabled?: boolean;
  ariaLabel?: string;
}

export interface SegmentedControlProps<TValue extends string = string>
  extends
    Omit<HTMLAttributes<HTMLDivElement>, 'onChange' | 'defaultValue'>,
    VariantProps<typeof segmentedControlVariants> {
  options: ReadonlyArray<SegmentedControlOption<TValue>>;
  value: TValue;
  onValueChange: (next: TValue) => void;
  disabled?: boolean;
  ariaLabel: string;
  name?: string;
}

function findEnabledIndex<TValue extends string>(
  options: ReadonlyArray<SegmentedControlOption<TValue>>,
  start: number,
  step: 1 | -1,
): number {
  const len = options.length;
  if (len === 0) return start;
  for (let i = 1; i <= len; i += 1) {
    const idx = (((start + step * i) % len) + len) % len;
    if (!options[idx]?.disabled) return idx;
  }
  return start;
}

export const SegmentedControl = forwardRef(function SegmentedControl<TValue extends string>(
  {
    options,
    value,
    onValueChange,
    disabled = false,
    ariaLabel,
    className,
    size,
    fullWidth,
    name,
    ...rest
  }: SegmentedControlProps<TValue>,
  ref: React.ForwardedRef<HTMLDivElement>,
) {
  const buttonRefs = useRef<Array<HTMLButtonElement | null>>([]);

  const focusIndex = useCallback((idx: number) => {
    buttonRefs.current[idx]?.focus();
  }, []);

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) return;
    const currentIdx = options.findIndex((o) => o.value === value);
    if (currentIdx < 0) return;

    let nextIdx: number | null = null;
    switch (e.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        nextIdx = findEnabledIndex(options, currentIdx, 1);
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        nextIdx = findEnabledIndex(options, currentIdx, -1);
        break;
      case 'Home': {
        const first = options.findIndex((o) => !o.disabled);
        nextIdx = first >= 0 ? first : null;
        break;
      }
      case 'End': {
        for (let i = options.length - 1; i >= 0; i -= 1) {
          if (!options[i]!.disabled) {
            nextIdx = i;
            break;
          }
        }
        break;
      }
      case ' ':
      case 'Enter':
        // текущий radio уже выбран, но явная активация по ARIA APG — no-op.
        e.preventDefault();
        return;
      default:
        return;
    }

    if (nextIdx !== null && nextIdx !== currentIdx) {
      e.preventDefault();
      const nextOption = options[nextIdx]!;
      onValueChange(nextOption.value);
      focusIndex(nextIdx);
    }
  };

  return (
    <div
      ref={ref}
      role="radiogroup"
      aria-label={ariaLabel}
      aria-disabled={disabled || undefined}
      tabIndex={-1}
      onKeyDown={handleKeyDown}
      className={cn(segmentedControlVariants({ size, fullWidth }), className)}
      {...rest}
    >
      {options.map((opt, idx) => {
        const isSelected = opt.value === value;
        const isDisabled = disabled || opt.disabled === true;
        return (
          <button
            key={opt.value}
            ref={(el) => {
              buttonRefs.current[idx] = el;
            }}
            type="button"
            role="radio"
            aria-checked={isSelected}
            aria-disabled={isDisabled || undefined}
            aria-label={opt.ariaLabel}
            tabIndex={isSelected ? 0 : -1}
            disabled={isDisabled}
            data-value={opt.value}
            data-name={name}
            onClick={() => {
              if (isDisabled || isSelected) return;
              onValueChange(opt.value);
            }}
            className={cn(segmentedItemVariants({ size, fullWidth }))}
          >
            {opt.icon ? (
              <span aria-hidden="true" className="inline-flex">
                {opt.icon}
              </span>
            ) : null}
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}) as unknown as (<TValue extends string>(
  props: SegmentedControlProps<TValue> & { ref?: React.ForwardedRef<HTMLDivElement> },
) => JSX.Element) & { displayName: string };

SegmentedControl.displayName = 'SegmentedControl';

export { segmentedControlVariants, segmentedItemVariants };
