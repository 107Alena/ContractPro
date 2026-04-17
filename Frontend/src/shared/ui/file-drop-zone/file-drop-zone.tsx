// FileDropZone (§8.3) — drag-and-drop загрузка одного файла с table-driven
// валидацией через @/shared/lib/validate-file. По умолчанию uncontrolled:
// держит локальные file/error/loading состояния и зовёт onAccepted/onError.
//
// A11y: drag-and-drop — mouse-only (как и в нативном UA). Клавиатурный путь
// — через видимую кнопку «Выбрать файл» (real <button>). Поэтому root —
// region (без role=button) с aria-label, чтобы избежать nested interactive
// (axe wcag2a violation aria-allowed-role).
import { cva, type VariantProps } from 'class-variance-authority';
import {
  forwardRef,
  type ReactNode,
  useCallback,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from 'react';
import { type FileRejection, useDropzone } from 'react-dropzone';

import { getActiveFormats, getDropzoneAccept, MAX_FILE_SIZE } from '@/shared/config/file-formats';
import { type FeatureFlags } from '@/shared/config/runtime-env';
import { cn } from '@/shared/lib/cn';
import {
  FileValidationError,
  getFileValidationMessage,
  validateFile,
} from '@/shared/lib/validate-file';
import { Button } from '@/shared/ui/button';
import { Spinner } from '@/shared/ui/spinner';

const dropZoneVariants = cva(
  [
    'relative flex flex-col items-center justify-center gap-3 w-full',
    'rounded-md border-2 border-dashed text-center',
    'px-6 py-10 transition-colors duration-150',
    'focus-within:outline-none focus-within:ring focus-within:ring-offset-2',
    'select-none',
  ],
  {
    variants: {
      state: {
        idle: 'border-border bg-bg hover:bg-bg-muted hover:border-brand-500',
        dragActive: 'border-brand-500 bg-brand-50',
        dragReject: 'border-danger bg-danger/5',
        selected: 'border-brand-500 bg-brand-50',
        error: 'border-danger bg-danger/5',
        loading: 'border-border bg-bg-muted cursor-progress',
        disabled: 'border-border bg-bg-muted opacity-60 cursor-not-allowed',
      },
    },
    defaultVariants: { state: 'idle' },
  },
);

export type FileDropZoneState = NonNullable<VariantProps<typeof dropZoneVariants>['state']>;

export interface FileDropZoneHandle {
  /** Программно открыть нативный file-picker. */
  open: () => void;
  /**
   * Сбросить выбранный файл и ошибку. onReset вызывается только если до
   * вызова что-то было выбрано — чтобы родитель не получал ложных уведомлений.
   */
  reset: () => void;
}

export interface FileDropZoneProps {
  /** Вызывается, когда файл прошёл валидацию. */
  onAccepted?: (file: File) => void;
  /** Вызывается при ошибке (валидация или react-dropzone отклонение). */
  onError?: (error: FileValidationError) => void;
  /** Вызывается при сбросе ранее выбранного файла. */
  onReset?: () => void;
  /** Файл по умолчанию (Storybook / тесты). Не controlled-режим. */
  defaultFile?: File;
  /** Лимит размера в байтах. По умолчанию MAX_FILE_SIZE = 20 МБ. */
  maxSize?: number;
  /**
   * Переопределение feature-flags (тесты). По умолчанию читается из
   * window.__ENV__.FEATURES. **Передавайте мемоизированный объект** — иначе
   * accept/dropzone-handlers будут пересоздаваться на каждый ререндер.
   */
  featureFlags?: FeatureFlags;
  /** Полностью отключает компонент. */
  disabled?: boolean;
  /** Внешнее loading-состояние (например, идёт upload). */
  loading?: boolean;
  /** Кастомный заголовок idle-состояния. */
  idleTitle?: ReactNode;
  /** Кастомное описание idle-состояния. */
  idleHint?: ReactNode;
  /** Дополнительные классы для root. */
  className?: string;
  /** id корня (для aria-describedby у label). */
  id?: string;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} Б`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(0)} КБ`;
  return `${(kb / 1024).toFixed(1)} МБ`;
}

function describedBy(...ids: (string | undefined)[]): string | undefined {
  const present = ids.filter((id): id is string => Boolean(id));
  return present.length === 0 ? undefined : present.join(' ');
}

export const FileDropZone = forwardRef<FileDropZoneHandle, FileDropZoneProps>(function FileDropZone(
  {
    onAccepted,
    onError,
    onReset,
    defaultFile,
    maxSize = MAX_FILE_SIZE,
    featureFlags,
    disabled = false,
    loading = false,
    idleTitle = 'Перетащите файл сюда',
    idleHint,
    className,
    id,
  },
  ref,
) {
  const [file, setFile] = useState<File | undefined>(defaultFile);
  const [error, setError] = useState<FileValidationError | undefined>(undefined);
  const [isValidating, setIsValidating] = useState(false);
  // Возрастающий ID для отброса stale-результатов асинхронной validateFile,
  // если пользователь успел кинуть второй файл / нажать Reset за время
  // FileReader+проверки magic-bytes (актуально при медленном диске).
  const validationIdRef = useRef(0);

  const formats = useMemo(() => getActiveFormats(featureFlags), [featureFlags]);
  const accept = useMemo(() => getDropzoneAccept(formats), [formats]);
  const allowedLabels = useMemo(() => formats.map((f) => f.label).join(', '), [formats]);
  const maxSizeMb = Math.max(1, Math.round(maxSize / 1024 / 1024));
  const isBusy = loading || isValidating;
  const isDisabled = disabled || isBusy;

  const handleDrop = useCallback(
    async (accepted: File[], rejections: FileRejection[]) => {
      const requestId = ++validationIdRef.current;
      if (rejections.length > 0) {
        const r = rejections[0];
        const fileError = r?.errors[0];
        const code =
          fileError?.code === 'file-invalid-type'
            ? 'UNSUPPORTED_FORMAT'
            : fileError?.code === 'file-too-large'
              ? 'FILE_TOO_LARGE'
              : 'INVALID_FILE';
        const err = new FileValidationError(code, { allowed: formats.map((f) => f.label) });
        setFile(undefined);
        setError(err);
        onError?.(err);
        return;
      }
      const next = accepted[0];
      if (!next) return;

      setIsValidating(true);
      try {
        await validateFile(next, { maxSize, formats });
        if (validationIdRef.current !== requestId) return;
        setFile(next);
        setError(undefined);
        onAccepted?.(next);
      } catch (e) {
        if (validationIdRef.current !== requestId) return;
        const err = e instanceof FileValidationError ? e : new FileValidationError('INVALID_FILE');
        setFile(undefined);
        setError(err);
        onError?.(err);
      } finally {
        if (validationIdRef.current === requestId) setIsValidating(false);
      }
    },
    [formats, maxSize, onAccepted, onError],
  );

  const { getRootProps, getInputProps, isDragActive, isDragReject, open, inputRef } = useDropzone({
    onDrop: handleDrop,
    accept,
    multiple: false,
    maxFiles: 1,
    disabled: isDisabled,
    // Клик/клавиатура переключаются на внутреннюю real-кнопку — так избегаем
    // nested interactive (root + button), сохраняя клавиатурную доступность.
    noClick: true,
    noKeyboard: true,
  });

  const resetInternal = useCallback(
    (silent = false) => {
      // Инвалидируем in-flight validation, если есть.
      validationIdRef.current++;
      const hadFile = file !== undefined || error !== undefined;
      setFile(undefined);
      setError(undefined);
      if (inputRef.current) inputRef.current.value = '';
      if (!silent && hadFile) onReset?.();
    },
    [file, error, inputRef, onReset],
  );

  useImperativeHandle(
    ref,
    () => ({
      open: () => {
        if (isDisabled) return;
        open();
      },
      reset: () => resetInternal(),
    }),
    [open, isDisabled, resetInternal],
  );

  const state: FileDropZoneState = disabled
    ? 'disabled'
    : isBusy
      ? 'loading'
      : error
        ? 'error'
        : isDragReject
          ? 'dragReject'
          : isDragActive
            ? 'dragActive'
            : file
              ? 'selected'
              : 'idle';

  const baseId = id ?? 'file-drop-zone';
  const errorId = error ? `${baseId}-error` : undefined;
  const showHint =
    state === 'idle' || state === 'dragActive' || state === 'dragReject' || state === 'disabled';
  const hintId = showHint ? `${baseId}-hint` : undefined;
  // role=region вместо role=button — drag-and-drop без клавиатурной активации
  // (клавиатура идёт через внутреннюю кнопку), root остаётся drop-target.
  const rootProps = getRootProps({
    id,
    className: cn(dropZoneVariants({ state }), className),
    'aria-label': 'Поле загрузки файла',
    'aria-disabled': isDisabled || undefined,
    'aria-invalid': error ? true : undefined,
    'aria-describedby': describedBy(hintId, errorId),
    'data-state': state,
    role: 'region',
  });

  return (
    <div className="flex flex-col gap-2">
      <div {...rootProps}>
        <input {...getInputProps()} aria-label="Файл договора" />

        {state === 'loading' && (
          <>
            <Spinner size="md" />
            <p className="text-sm text-fg">Проверяем файл…</p>
          </>
        )}

        {state === 'selected' && file && (
          <>
            <p className="text-sm font-medium text-fg break-all">{file.name}</p>
            <p className="text-xs text-fg-muted">{formatBytes(file.size)}</p>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                resetInternal();
              }}
            >
              Удалить
            </Button>
          </>
        )}

        {(state === 'idle' ||
          state === 'dragActive' ||
          state === 'dragReject' ||
          state === 'disabled') && (
          <>
            <p className="text-base font-medium text-fg">
              {state === 'dragActive' && !isDragReject
                ? 'Отпустите файл'
                : state === 'dragReject'
                  ? 'Этот файл нельзя загрузить'
                  : idleTitle}
            </p>
            <p className="text-sm text-fg-muted" id={hintId}>
              {idleHint ??
                `Поддерживается: ${allowedLabels}. Максимальный размер: ${maxSizeMb} МБ.`}
            </p>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              disabled={isDisabled}
              onClick={(e) => {
                e.stopPropagation();
                open();
              }}
            >
              Выбрать файл
            </Button>
          </>
        )}

        {state === 'error' && (
          <>
            <p className="text-base font-medium text-fg">Загрузка не выполнена</p>
            <p id={errorId} className="text-sm text-danger" role="alert" aria-live="polite">
              {error ? getFileValidationMessage(error) : ''}
            </p>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={(e) => {
                e.stopPropagation();
                setError(undefined);
                open();
              }}
            >
              Выбрать другой файл
            </Button>
          </>
        )}
      </div>
    </div>
  );
});

export { dropZoneVariants };
