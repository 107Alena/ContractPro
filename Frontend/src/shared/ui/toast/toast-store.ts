import { create } from 'zustand';

/**
 * Headless toast store. UI (`<Toaster/>`) подписывается на `toasts` и рендерит
 * Radix Toast Items. API `toast.*` (см. `use-toast.ts`) — единственный producer.
 *
 * FIFO 5: при переполнении отбрасывается самый старый не-sticky toast. Sticky
 * (`duration: null`) не вытесняются.
 * ADR-FE-20 (overlays): Zustand вне React-дерева — чтобы toast'ы вызывались из
 * query-слоя (api/error-toaster) без prop-drilling.
 */
export type ToastVariant = 'success' | 'error' | 'warning' | 'info' | 'sticky';

export interface ToastAction {
  label: string;
  onClick: (toastId: string) => void;
}

export interface ToastRecord {
  id: string;
  variant: ToastVariant;
  title: string;
  description?: string;
  /** `null` — sticky (не автоскрывается). Число — мс. */
  duration: number | null;
  action?: ToastAction;
}

export interface ToastInput {
  id?: string;
  title: string;
  description?: string;
  duration?: number | null;
  action?: ToastAction;
}

export const TOAST_LIMIT = 5;
export const DEFAULT_TOAST_DURATION = 5000;

interface ToastStoreState {
  toasts: ToastRecord[];
  add: (variant: ToastVariant, input: ToastInput) => string;
  dismiss: (id: string) => void;
  clear: () => void;
}

let counter = 0;
function nextId(): string {
  counter += 1;
  return `toast-${counter}-${Date.now()}`;
}

function durationFor(variant: ToastVariant, input: ToastInput): number | null {
  if (input.duration !== undefined) return input.duration;
  if (variant === 'sticky') return null;
  return DEFAULT_TOAST_DURATION;
}

export const useToastStore = create<ToastStoreState>((set) => ({
  toasts: [],
  add(variant, input) {
    const id = input.id ?? nextId();
    set((state) => {
      const next: ToastRecord = {
        id,
        variant,
        title: input.title,
        duration: durationFor(variant, input),
        ...(input.description !== undefined && { description: input.description }),
        ...(input.action !== undefined && { action: input.action }),
      };
      const combined = [...state.toasts, next];
      if (combined.length <= TOAST_LIMIT) return { toasts: combined };
      // Вытесняем самый старый не-sticky.
      const excess = combined.length - TOAST_LIMIT;
      const keepers: ToastRecord[] = [];
      let dropped = 0;
      for (const t of combined) {
        if (dropped < excess && t.duration !== null) {
          dropped += 1;
          continue;
        }
        keepers.push(t);
      }
      // Если все оставшиеся — sticky, и нового place нет, trim from head любые.
      if (keepers.length > TOAST_LIMIT) {
        return { toasts: keepers.slice(keepers.length - TOAST_LIMIT) };
      }
      return { toasts: keepers };
    });
    return id;
  },
  dismiss(id) {
    set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) }));
  },
  clear() {
    set({ toasts: [] });
  },
}));

/** Internal — reset counter для детерминированных тестов. */
export function __resetToastStoreForTests(): void {
  counter = 0;
  useToastStore.setState({ toasts: [] });
}
