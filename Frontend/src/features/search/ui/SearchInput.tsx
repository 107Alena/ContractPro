import { forwardRef, type InputHTMLAttributes, type ReactNode, useId } from 'react';

import { cn } from '@/shared/lib/cn';
import { Input } from '@/shared/ui/input';
import { Spinner } from '@/shared/ui/spinner';

export interface SearchInputProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'value' | 'onChange' | 'size'
> {
  value: string;
  onValueChange: (next: string) => void;
  placeholder?: string;
  /** Показывать иконку «крестик» для быстрого сброса (когда есть значение). */
  clearable?: boolean;
  /** Локализованный aria-label для кнопки сброса. */
  clearLabel?: string;
  /** true — показать spinner вместо иконки поиска (debounce pending). */
  isPending?: boolean;
  /** Иконка (например лупа). По умолчанию SVG лупа. */
  leadingIcon?: ReactNode;
  /** Ручная обёртка aria-label'а. */
  ariaLabel?: string;
}

function SearchIcon() {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 16 16"
      width="14"
      height="14"
      fill="none"
      className="text-fg-muted"
    >
      <circle cx="7" cy="7" r="4.5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M10.5 10.5 14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function ClearIcon() {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 16 16"
      width="12"
      height="12"
      fill="none"
    >
      <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
    </svg>
  );
}

export const SearchInput = forwardRef<HTMLInputElement, SearchInputProps>(function SearchInput(
  {
    value,
    onValueChange,
    placeholder = 'Поиск…',
    clearable = true,
    clearLabel = 'Очистить',
    isPending = false,
    leadingIcon,
    className,
    ariaLabel,
    disabled,
    onKeyDown,
    ...rest
  },
  ref,
) {
  const inputId = useId();
  const showClear = clearable && value.length > 0 && !disabled;

  return (
    <div className={cn('relative flex items-center', className)}>
      <span className="pointer-events-none absolute left-3 flex items-center">
        {isPending ? <Spinner size="sm" /> : (leadingIcon ?? <SearchIcon />)}
      </span>
      <Input
        ref={ref}
        id={inputId}
        type="search"
        role="searchbox"
        value={value}
        onChange={(e) => onValueChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape' && showClear) {
            e.preventDefault();
            onValueChange('');
          }
          onKeyDown?.(e);
        }}
        placeholder={placeholder}
        aria-label={ariaLabel ?? placeholder}
        disabled={disabled}
        className="pl-9 pr-9"
        {...rest}
      />
      {showClear ? (
        <button
          type="button"
          aria-label={clearLabel}
          onClick={() => onValueChange('')}
          className={cn(
            'absolute right-2 inline-flex h-6 w-6 items-center justify-center rounded-sm',
            'text-fg-muted hover:text-fg hover:bg-bg-muted',
            'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
          )}
        >
          <ClearIcon />
        </button>
      ) : null}
    </div>
  );
});
