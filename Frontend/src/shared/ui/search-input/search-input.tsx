import {
  type ChangeEvent,
  forwardRef,
  type InputHTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';

import { cn } from '@/shared/lib/cn';
import { useDebouncedCallback } from '@/shared/lib/use-debounce';
import { Input } from '@/shared/ui/input';
import { Spinner } from '@/shared/ui/spinner';

export interface SearchInputProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'value' | 'onChange' | 'size'
> {
  value: string;
  /** Вызывается после debounce (см. debounceMs). По умолчанию — синхронно при вводе. */
  onValueChange: (next: string) => void;
  placeholder?: string;
  /** Показывать иконку «крестик» для быстрого сброса (когда есть значение). */
  clearable?: boolean;
  /** Локализованный aria-label для кнопки сброса. */
  clearLabel?: string;
  /** true — показать spinner вместо иконки поиска (debounce pending). */
  isPending?: boolean;
  /** Иконка (например, лупа). По умолчанию — SVG лупа. */
  leadingIcon?: ReactNode;
  /** Ручная обёртка aria-label'а. */
  ariaLabel?: string;
  /** Задержка до вызова onValueChange в мс. 0 (default) — синхронно. */
  debounceMs?: number;
  /** Срабатывает синхронно при каждом изменении (удобно для внешнего is-pending индикатора). */
  onInputChange?: (next: string) => void;
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
    debounceMs = 0,
    onInputChange,
    ...rest
  },
  ref,
) {
  const inputId = useId();
  const [draft, setDraft] = useState(value);
  // Что инпут отправил в onValueChange (в т.ч. pending debounce). Нужен, чтобы
  // отличать «внешнее» обновление value от «эха» нашего собственного onValueChange.
  const emittedRef = useRef(value);

  useEffect(() => {
    if (value !== emittedRef.current) {
      emittedRef.current = value;
      setDraft(value);
    }
  }, [value]);

  const debouncedEmit = useDebouncedCallback(onValueChange, debounceMs);

  const emitChange = useCallback(
    (next: string, { immediate = false }: { immediate?: boolean } = {}) => {
      setDraft(next);
      emittedRef.current = next;
      onInputChange?.(next);
      if (immediate || debounceMs <= 0) {
        debouncedEmit.cancel();
        onValueChange(next);
      } else {
        debouncedEmit(next);
      }
    },
    [debounceMs, debouncedEmit, onValueChange, onInputChange],
  );

  const handleChange = (e: ChangeEvent<HTMLInputElement>) => emitChange(e.target.value);

  const showClear = clearable && draft.length > 0 && !disabled;

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape' && showClear) {
      e.preventDefault();
      emitChange('', { immediate: true });
    }
    onKeyDown?.(e);
  };

  const handleClear = () => emitChange('', { immediate: true });

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
        value={draft}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
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
          onClick={handleClear}
          className={cn(
            'absolute right-2 inline-flex h-6 w-6 items-center justify-center rounded-sm',
            'text-fg-muted hover:bg-bg-muted hover:text-fg',
            'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
          )}
        >
          <ClearIcon />
        </button>
      ) : null}
    </div>
  );
});
