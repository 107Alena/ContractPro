// NewCheckPage (§17.4/§16.2 — FE-TASK-043) — экран «Новая проверка договора»
// с 12 состояниями Figma. Оркеструет feature/contract-upload и делит экран
// на форму (title + FileDropZone) и информационные виджеты (WillHappenSteps,
// WhatWeCheck).
//
// Архитектура:
//   - useUploadContract (feature) — mutation + abortController + setError.
//     Модалка подтверждения типа (FR-2.1.3 / FE-TASK-037) НЕ монтируется тут:
//     Provider живёт в App-shell (см. App.tsx), SSE глобальный — модалка
//     всплывёт автоматически независимо от текущей страницы.
//   - ProcessingProgress (widget) — inline показ после 202/UPLOADED. Финальное
//     пребывание пользователя с прогрессом — на ResultPage (через SSE);
//     мы редиректим туда сразу после успешной мутации (§16.2 sequence).
//   - Tabs (upload | paste-text) — для v1 вкладка «Текст» видна как placeholder:
//     backend поддерживает только PDF (ADR-FE-01, §7.5), текстовый ввод
//     появится в v1.1. Табы оставлены для согласования с Figma и чтобы не
//     менять layout при будущем включении.
//   - State: form-поля (title/file) держим в useState — в проекте RHF ещё
//     не введён (FE-TASK-025). Адаптер `setError` совместим с
//     `UseFormSetErrorLike` в `useUploadContract`.
//   - RBAC: <RequireRole>-guard не нужен — permission `contract.upload` есть у
//     всех ролей (§5.5). Гарантия: route уже под AppLayout (auth).
//
// Состояния 12 (AC + Figma 4. Новая проверка):
//   1. idle — пустая форма, default.
//   2. title-filled — title заполнен, файл ещё не выбран.
//   3. drag-hover — файл над drop-зоной.
//   4. file-selected — файл валиден, submit активен.
//   5. error-file-too-large — inline в FileDropZone (413 / maxSize).
//   6. error-file-wrong-format — inline (415 / accept-check).
//   7. error-invalid-file — inline (400 INVALID_FILE).
//   8. submitting — upload идёт, прогресс, submit disabled.
//   9. processing-start — 202 UPLOADED, ProcessingProgress виден, ждём redirect.
//  10. upload-error — generic (5xx, сеть) — toast, форма остаётся заполненной.
//  11. low-confidence-type — модалка от глобального Provider (FE-TASK-037).
//  12. rbac-restricted — пользователь без `contract.upload` (не достигается в v1,
//       т.к. permission есть у всех ролей, но story существует для полноты).
import { type FormEvent, useCallback, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import {
  type UploadFormValues,
  useUploadContract,
} from '@/features/contract-upload';
import { OrchestratorError, type UseFormSetErrorLike } from '@/shared/api';
import { useCan } from '@/shared/auth';
import { Button, Input, Label, toast } from '@/shared/ui';
import { FileDropZone, type FileDropZoneHandle } from '@/shared/ui/file-drop-zone';
import { WhatWeCheck } from '@/widgets/new-check-what-we-check';
import { WillHappenSteps } from '@/widgets/new-check-will-happen';
import { ProcessingProgress } from '@/widgets/processing-progress';

type TabKey = 'upload' | 'paste';

interface FormState {
  title: string;
  file: File | null;
  titleError: string | null;
  fileError: string | null;
  formError: string | null;
}

const INITIAL_STATE: FormState = {
  title: '',
  file: null,
  titleError: null,
  fileError: null,
  formError: null,
};

export function NewCheckPage(): JSX.Element {
  const navigate = useNavigate();
  const canUpload = useCan('contract.upload');
  const dropRef = useRef<FileDropZoneHandle>(null);
  const [state, setState] = useState<FormState>(INITIAL_STATE);
  const [activeTab, setActiveTab] = useState<TabKey>('upload');
  const [uploadFraction, setUploadFraction] = useState<number | null>(null);

  // Adapter под UseFormSetErrorLike — `useUploadContract` пробрасывает file-field
  // коды (413/415/400) и VALIDATION_ERROR на этот setter. Поле 'file' видим в
  // UploadFormValues, но показываем ошибку через FileDropZone.error-state,
  // которое он держит сам — поэтому здесь дублируем в `state.fileError` для
  // aria-describedby под input'ом.
  const setFieldError = useCallback<UseFormSetErrorLike<UploadFormValues>>(
    (name, error) => {
      setState((prev) => {
        if (name === 'file') {
          return { ...prev, fileError: error.message };
        }
        if (name === 'title') {
          return { ...prev, titleError: error.message };
        }
        return prev;
      });
    },
    [],
  );

  const { upload, isPending, reset } = useUploadContract<UploadFormValues>({
    setError: setFieldError,
    onUploadProgress: (p) => {
      if (p.fraction !== undefined) setUploadFraction(p.fraction);
    },
    onSuccess: (data) => {
      // §16.2: редирект на ResultPage — там ProcessingProgress управляется SSE.
      navigate(`/contracts/${data.contractId}/versions/${data.versionId}/result`);
    },
    onError: (err, msg) => {
      setUploadFraction(null);
      // File-field (413/415/400) и VALIDATION_ERROR уже легли в state через setError.
      // Для generic (5xx, сеть, offline) — показываем toast + form-banner.
      const isFieldError =
        err instanceof OrchestratorError &&
        (err.error_code === 'FILE_TOO_LARGE' ||
          err.error_code === 'UNSUPPORTED_FORMAT' ||
          err.error_code === 'INVALID_FILE' ||
          err.error_code === 'VALIDATION_ERROR');
      if (!isFieldError) {
        toast.error({ title: msg.title, ...(msg.hint && { description: msg.hint }) });
        setState((prev) => ({ ...prev, formError: msg.title }));
      }
    },
  });

  const handleTitleChange = (value: string): void => {
    setState((prev) => ({ ...prev, title: value, titleError: null, formError: null }));
  };

  const handleFileAccepted = (file: File): void => {
    setState((prev) => ({ ...prev, file, fileError: null, formError: null }));
  };

  // FileDropZone сам рендерит человекочитаемое сообщение client-side
  // валидации (см. getFileValidationMessage). Мы только обнуляем выбранный
  // файл — server-side ошибки приходят отдельно через useUploadContract.setError.
  const handleFileError = (): void => {
    setState((prev) => ({ ...prev, file: null }));
  };

  const handleFileReset = (): void => {
    setState((prev) => ({ ...prev, file: null, fileError: null }));
  };

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault();
    const title = state.title.trim();
    // Client-side pre-check: оба поля обязательны (см. UploadContractInput).
    // Серверная валидация параллельно перехватит VALIDATION_ERROR, если
    // что-то проскочит (например, пустой title после trim'а).
    if (!title) {
      setState((prev) => ({ ...prev, titleError: 'Укажите название договора' }));
      return;
    }
    if (!state.file) {
      setState((prev) => ({
        ...prev,
        fileError: prev.fileError ?? 'Выберите файл договора',
      }));
      return;
    }
    reset();
    setUploadFraction(0);
    upload({ file: state.file, title });
  };

  // RBAC-fallback (story #12). В v1 не достигается — permission есть у всех
  // ролей. Оставляем на случай будущих изменений §5.5 или debug-режима.
  if (!canUpload) {
    return (
      <main
        data-testid="page-new-check"
        className="mx-auto flex min-h-[60vh] max-w-3xl flex-col items-center gap-3 px-6 py-12 text-center"
      >
        <h1 className="text-2xl font-semibold text-fg">Недостаточно прав</h1>
        <p className="text-base text-fg-muted">
          Загрузка договоров доступна юристам, бизнес-пользователям и администраторам
          организации. Обратитесь к администратору, если нужен доступ.
        </p>
      </main>
    );
  }

  const canSubmit = Boolean(state.title.trim()) && Boolean(state.file) && !isPending;
  const submitting = isPending;

  return (
    <main
      data-testid="page-new-check"
      className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold text-fg">Новая проверка</h1>
        <p className="text-sm text-fg-muted">
          Загрузите PDF договора — через 1–2 минуты получите отчёт с рисками и
          рекомендациями.
        </p>
      </header>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <form
          aria-label="Загрузка договора"
          data-testid="new-check-form"
          onSubmit={handleSubmit}
          className="flex flex-col gap-5 rounded-md border border-border bg-bg p-5 shadow-sm"
          noValidate
        >
          <Tabs active={activeTab} onChange={setActiveTab} disabled={submitting} />

          {activeTab === 'upload' ? (
            <>
              <div className="flex flex-col gap-2">
                <Label htmlFor="new-check-title" required>
                  Название договора
                </Label>
                <Input
                  id="new-check-title"
                  name="title"
                  value={state.title}
                  onChange={(e) => handleTitleChange(e.target.value)}
                  placeholder="Например: Договор аренды офиса № 42"
                  error={Boolean(state.titleError)}
                  aria-describedby={state.titleError ? 'new-check-title-error' : undefined}
                  disabled={submitting}
                  required
                  maxLength={200}
                />
                {state.titleError ? (
                  <p
                    id="new-check-title-error"
                    className="text-sm text-danger"
                    role="alert"
                  >
                    {state.titleError}
                  </p>
                ) : null}
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="new-check-file" required>
                  Файл договора
                </Label>
                <FileDropZone
                  ref={dropRef}
                  id="new-check-file"
                  loading={submitting}
                  disabled={submitting}
                  onAccepted={handleFileAccepted}
                  onError={handleFileError}
                  onReset={handleFileReset}
                />
                {state.fileError ? (
                  <p
                    id="new-check-file-error"
                    className="text-sm text-danger"
                    role="alert"
                  >
                    {state.fileError}
                  </p>
                ) : null}
              </div>

              {submitting ? (
                <ProcessingProgress
                  status="UPLOADED"
                  aria-label="Загрузка договора"
                  className="mt-1"
                  // Прогресс-байт из axios (0..1) отражает лишь upload-фазу;
                  // pipeline-шаги (QUEUED → ... → READY) уже после 202.
                  // Используем UPLOADED как «договор загружается».
                />
              ) : null}

              {uploadFraction !== null && submitting ? (
                <p className="text-xs text-fg-muted" aria-live="polite">
                  Отправлено {Math.round(uploadFraction * 100)}%
                </p>
              ) : null}

              {state.formError ? (
                <p
                  className="rounded-md border border-danger/40 bg-danger/5 p-3 text-sm text-danger"
                  role="alert"
                >
                  {state.formError}
                </p>
              ) : null}

              <div className="flex flex-wrap items-center justify-end gap-3">
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => navigate('/dashboard')}
                  disabled={submitting}
                >
                  Отмена
                </Button>
                <Button
                  type="submit"
                  variant="primary"
                  disabled={!canSubmit}
                  loading={submitting}
                >
                  Начать проверку
                </Button>
              </div>
            </>
          ) : (
            <PasteTextPlaceholder />
          )}
        </form>

        <aside className="flex flex-col gap-4">
          <WillHappenSteps />
          <WhatWeCheck />
        </aside>
      </div>
    </main>
  );
}

/** Табы upload ↔ paste (§17.4 «PasteTextTab (опц., через табы)»). Нативные
 *  button-ы с role=tab; panel — родительская форма. Минимальная реализация
 *  без Radix Tabs (не зависим от неустановленного пакета). */
interface TabsProps {
  active: TabKey;
  onChange: (key: TabKey) => void;
  disabled: boolean;
}

function Tabs({ active, onChange, disabled }: TabsProps): JSX.Element {
  return (
    <div
      role="tablist"
      aria-label="Способ загрузки"
      className="flex gap-1 rounded-md border border-border bg-bg-muted p-1"
    >
      <TabButton
        id="tab-upload"
        panelId="tab-panel-upload"
        label="Загрузить файл"
        selected={active === 'upload'}
        onClick={() => onChange('upload')}
        disabled={disabled}
      />
      <TabButton
        id="tab-paste"
        panelId="tab-panel-paste"
        label="Вставить текст"
        selected={active === 'paste'}
        onClick={() => onChange('paste')}
        disabled={disabled}
      />
    </div>
  );
}

interface TabButtonProps {
  id: string;
  panelId: string;
  label: string;
  selected: boolean;
  disabled: boolean;
  onClick: () => void;
}

function TabButton({
  id,
  panelId,
  label,
  selected,
  disabled,
  onClick,
}: TabButtonProps): JSX.Element {
  const base =
    'flex-1 rounded-sm px-3 py-1.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1 disabled:cursor-not-allowed disabled:opacity-60';
  const variant = selected
    ? 'bg-bg text-fg shadow-sm'
    : 'text-fg-muted hover:bg-bg hover:text-fg';
  return (
    <button
      id={id}
      type="button"
      role="tab"
      aria-selected={selected}
      aria-controls={panelId}
      tabIndex={selected ? 0 : -1}
      onClick={onClick}
      disabled={disabled}
      className={`${base} ${variant}`}
    >
      {label}
    </button>
  );
}

function PasteTextPlaceholder(): JSX.Element {
  return (
    <div
      id="tab-panel-paste"
      role="tabpanel"
      aria-labelledby="tab-paste"
      className="flex flex-col items-center gap-2 rounded-md border border-dashed border-border bg-bg-muted p-8 text-center"
    >
      <p className="text-sm font-medium text-fg">Вставка текста появится позже</p>
      <p className="text-sm text-fg-muted">
        В v1 поддерживается только загрузка PDF-файла. Вставка текста из буфера —
        в планах на следующий релиз.
      </p>
    </div>
  );
}
