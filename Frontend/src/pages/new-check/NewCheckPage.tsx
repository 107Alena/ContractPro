// NewCheckPage (Figma 112:2, FE-TASK-043) — экран «Новая проверка договора».
// Полная структура Figma (этап 4.6): PageIntro + FormatHint → full-width
// WorkspaceCard (табы + drop-зона/файл-карточка) → TwoColumnInfo (шаги +
// категории) → Tips → TrustFooter. Оркеструет feature/contract-upload.
//
// Архитектура:
//   - useUploadContract (feature) — mutation + abortController + setError.
//     Модалка подтверждения типа (FR-2.1.3 / FE-TASK-037) НЕ монтируется тут:
//     Provider живёт в App-shell (см. App.tsx), SSE глобальный — модалка
//     всплывёт автоматически независимо от текущей страницы.
//   - ProcessingProgress (widget) — inline показ после 202/UPLOADED. Финальное
//     пребывание пользователя с прогрессом — на ResultPage (через SSE);
//     мы редиректим туда сразу после успешной мутации (§16.2 sequence).
//   - Title: в Figma-флоу нет поля «Название договора», но бэкенд требует
//     обязательный `title` (multipart). Решение (scope 4.6): авто-вывод из
//     имени файла (без расширения) — пользователь видит имя файла в FileCard.
//   - Tabs (upload | paste-text) — вкладка «Текст» = placeholder: backend
//     поддерживает только PDF (ADR-FE-01, §7.5), текстовый ввод — в v1.1.
//   - State: file/title/ошибки держим в useState (RHF не введён, FE-TASK-025).
//   - Honesty-отклонения от Figma: «до 50 МБ» → «до 20 МБ» (реальный лимит,
//     openapi `макс. 20 МБ`); подзаголовок без обещания paste-ввода.
//   - Корень — <div> (AppLayout уже оборачивает Outlet в <main>; избегаем
//     вложенного landmark).
import { type FormEvent, useCallback, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import { type UploadFormValues, useUploadContract } from '@/features/contract-upload';
import { OrchestratorError, type UseFormSetErrorLike } from '@/shared/api';
import { useCan } from '@/shared/auth';
import { cn } from '@/shared/lib/cn';
import { Button, Card, toast } from '@/shared/ui';
import { FileDropZone, type FileDropZoneHandle } from '@/shared/ui/file-drop-zone';
import { TrustFooter } from '@/widgets/dashboard-trust-footer';
import { Tips } from '@/widgets/new-check-tips';
import { WhatWeCheck } from '@/widgets/new-check-what-we-check';
import { WillHappenSteps } from '@/widgets/new-check-will-happen';
import { ProcessingProgress } from '@/widgets/processing-progress';

type TabKey = 'upload' | 'paste';

interface FormState {
  file: File | null;
  /** Авто-выведенное из имени файла название (отправляется на сервер). */
  title: string;
  fileError: string | null;
  formError: string | null;
}

const INITIAL_STATE: FormState = {
  file: null,
  title: '',
  fileError: null,
  formError: null,
};

/** Название договора из имени файла: отрезаем расширение, пустое → запасной. */
function deriveTitle(fileName: string): string {
  const stripped = fileName.replace(/\.[^./\\]+$/, '').trim();
  return stripped || 'Договор';
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} Б`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(0)} КБ`;
  return `${(kb / 1024).toFixed(1)} МБ`;
}

export function NewCheckPage(): JSX.Element {
  const navigate = useNavigate();
  const canUpload = useCan('contract.upload');
  const dropRef = useRef<FileDropZoneHandle>(null);
  const [state, setState] = useState<FormState>(INITIAL_STATE);
  const [activeTab, setActiveTab] = useState<TabKey>('upload');
  const [uploadFraction, setUploadFraction] = useState<number | null>(null);

  // Adapter под UseFormSetErrorLike — `useUploadContract` пробрасывает file-field
  // коды (413/415/400) и VALIDATION_ERROR. Поля title нет в UI → серверную
  // ошибку title показываем как form-banner.
  const setFieldError = useCallback<UseFormSetErrorLike<UploadFormValues>>((name, error) => {
    setState((prev) => {
      if (name === 'file') return { ...prev, fileError: error.message };
      if (name === 'title') return { ...prev, formError: error.message };
      return prev;
    });
  }, []);

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

  const handleFileAccepted = (file: File): void => {
    setState((prev) => ({
      ...prev,
      file,
      title: deriveTitle(file.name),
      fileError: null,
      formError: null,
    }));
  };

  // FileDropZone сам рендерит человекочитаемое сообщение client-side валидации.
  // Мы только обнуляем выбранный файл — server-side ошибки приходят отдельно.
  const handleFileError = (): void => {
    setState((prev) => ({ ...prev, file: null, title: '' }));
  };

  const handleFileReset = (): void => {
    setState((prev) => ({ ...prev, file: null, title: '', fileError: null }));
  };

  const handleSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault();
    if (!state.file) {
      setState((prev) => ({
        ...prev,
        fileError: prev.fileError ?? 'Выберите файл договора',
      }));
      return;
    }
    reset();
    setUploadFraction(0);
    upload({ file: state.file, title: state.title || deriveTitle(state.file.name) });
  };

  // RBAC-fallback. В v1 не достигается — permission `contract.upload` есть у
  // всех ролей. Оставляем на случай будущих изменений §5.5 или debug-режима.
  if (!canUpload) {
    return (
      <div
        data-testid="page-new-check"
        className="mx-auto flex min-h-[60vh] max-w-3xl flex-col items-center gap-3 px-6 py-12 text-center"
      >
        <h1 className="text-24 font-semibold text-fg">Недостаточно прав</h1>
        <p className="text-base text-fg-muted">
          Загрузка договоров доступна юристам, бизнес-пользователям и администраторам организации.
          Обратитесь к администратору, если нужен доступ.
        </p>
      </div>
    );
  }

  const submitting = isPending;
  const hasFile = Boolean(state.file);

  return (
    <div
      data-testid="page-new-check"
      className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <header className="flex flex-col gap-3">
        <h1 className="text-24 font-bold text-fg">Новая проверка договора</h1>
        <p className="max-w-[700px] text-15 leading-[22px] text-fg-muted">
          Загрузите PDF-файл — ContractPro определит тип документа, проверит обязательные условия и
          покажет риски с рекомендациями.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <span className="inline-flex items-center rounded-sm bg-brand-500/10 px-2 py-1 text-12 font-semibold text-brand-600">
            PDF
          </span>
          <p className="text-13 text-fg-disabled">
            На текущем этапе ContractPro поддерживает только PDF. Поддержка других форматов появится
            позже.
          </p>
        </div>
      </header>

      <Card
        as="section"
        aria-label="Загрузка договора"
        radius="xl"
        className="overflow-hidden border border-border-subtle shadow-none"
      >
        <Tabs active={activeTab} onChange={setActiveTab} disabled={submitting} />

        {activeTab === 'upload' ? (
          <form
            data-testid="new-check-form"
            onSubmit={handleSubmit}
            className="flex flex-col gap-5 p-6 md:p-8"
            noValidate
          >
            {/* Drop-зона остаётся смонтированной (хранит file-state + picker),
                визуально скрывается после выбора файла / во время отправки. */}
            <div className={cn((hasFile || submitting) && 'hidden')}>
              <FileDropZone
                ref={dropRef}
                id="new-check-file"
                idleTitle="Перетащите PDF сюда"
                loading={submitting}
                disabled={submitting}
                onAccepted={handleFileAccepted}
                onError={handleFileError}
                onReset={handleFileReset}
              />
              <p className="mt-3 flex items-center justify-center gap-1.5 text-12 text-fg-disabled">
                <span aria-hidden>🔒</span>
                Документы обрабатываются конфиденциально и не передаются третьим лицам
              </p>
            </div>

            {state.fileError ? (
              <p id="new-check-file-error" className="text-13 text-danger" role="alert">
                {state.fileError}
              </p>
            ) : null}

            {hasFile && !submitting && state.file ? (
              <FileCard
                file={state.file}
                onReplace={() => dropRef.current?.open()}
                onRemove={() => dropRef.current?.reset()}
              />
            ) : null}

            {submitting ? (
              <ProcessingProgress
                status="UPLOADED"
                aria-label="Загрузка договора"
                // Прогресс-байт из axios (0..1) отражает лишь upload-фазу;
                // pipeline-шаги (QUEUED → ... → READY) уже после 202.
                // Используем UPLOADED как «договор загружается».
              />
            ) : null}

            {uploadFraction !== null && submitting ? (
              <p className="text-12 text-fg-muted" aria-live="polite">
                Отправлено {Math.round(uploadFraction * 100)}%
              </p>
            ) : null}

            {state.formError ? (
              <p
                className="rounded-md border border-danger/40 bg-danger/5 p-3 text-13 text-danger"
                role="alert"
              >
                {state.formError}
              </p>
            ) : null}

            <div className="flex flex-wrap items-center justify-end gap-3">
              {hasFile ? (
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => dropRef.current?.open()}
                  disabled={submitting}
                >
                  Выбрать другой файл
                </Button>
              ) : (
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => navigate('/dashboard')}
                  disabled={submitting}
                >
                  Отмена
                </Button>
              )}
              <Button
                type="submit"
                variant="primary"
                disabled={!hasFile || submitting}
                loading={submitting}
              >
                Начать проверку
              </Button>
            </div>
          </form>
        ) : (
          <div className="p-6 md:p-8">
            <PasteTextPlaceholder />
          </div>
        )}
      </Card>

      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <WillHappenSteps />
        <WhatWeCheck />
      </div>

      <Tips />

      <TrustFooter />
    </div>
  );
}

/** FileCard — карточка выбранного PDF (Figma 135:3): тип-бейдж + имя + размер
 *  + действия «Заменить»/«Удалить» + зелёный статус готовности. */
interface FileCardProps {
  file: File;
  onReplace: () => void;
  onRemove: () => void;
}

function FileCard({ file, onReplace, onRemove }: FileCardProps): JSX.Element {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3.5 rounded-md bg-bg-muted px-4 py-3.5">
        <span
          aria-hidden
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-danger/10 text-11 font-bold text-danger"
        >
          PDF
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <p className="truncate text-14 font-medium text-fg">{file.name}</p>
          <p className="text-12 text-fg-subtle">PDF · {formatBytes(file.size)} · Загружен</p>
        </div>
        <div className="flex shrink-0 items-center gap-3 text-13 font-medium">
          <button
            type="button"
            onClick={onReplace}
            className="rounded-sm text-brand-500 hover:underline focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
          >
            Заменить
          </button>
          <button
            type="button"
            onClick={onRemove}
            className="rounded-sm text-fg-disabled hover:text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
          >
            Удалить
          </button>
        </div>
      </div>
      <div className="flex items-center justify-center gap-2">
        <span aria-hidden className="h-2 w-2 shrink-0 rounded-sm bg-success" />
        <p className="text-13 font-medium text-success" role="status">
          PDF загружен. Можно запускать проверку
        </p>
      </div>
    </div>
  );
}

/** Табы upload ↔ paste (Figma 116:3). Нативные button-ы с role=tab; panel —
 *  родительская форма/placeholder. Active — brand-подчёркивание. */
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
      className="flex gap-1 border-b border-border-subtle px-6"
    >
      <TabButton
        id="tab-upload"
        panelId="tab-panel-upload"
        icon="↑"
        label="Загрузить PDF"
        selected={active === 'upload'}
        onClick={() => onChange('upload')}
        disabled={disabled}
      />
      <TabButton
        id="tab-paste"
        panelId="tab-panel-paste"
        icon="✎"
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
  icon: string;
  label: string;
  selected: boolean;
  disabled: boolean;
  onClick: () => void;
}

function TabButton({
  id,
  panelId,
  icon,
  label,
  selected,
  disabled,
  onClick,
}: TabButtonProps): JSX.Element {
  const base =
    'inline-flex items-center gap-2 -mb-px border-b-2 px-4 py-3 text-14 transition-colors focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1 disabled:cursor-not-allowed disabled:opacity-60';
  const variant = selected
    ? 'border-brand-500 font-semibold text-brand-500'
    : 'border-transparent font-medium text-fg-subtle hover:text-fg';
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
      <span aria-hidden>{icon}</span>
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
      <p className="text-14 font-medium text-fg">Вставка текста появится позже</p>
      <p className="text-13 text-fg-muted">
        В v1 поддерживается только загрузка PDF-файла. Вставка текста из буфера — в планах на
        следующий релиз.
      </p>
    </div>
  );
}
