import { type ToastInput, type ToastVariant, useToastStore } from './toast-store';

function enqueue(variant: ToastVariant, input: ToastInput | string): string {
  const payload: ToastInput = typeof input === 'string' ? { title: input } : input;
  return useToastStore.getState().add(variant, payload);
}

/**
 * Императивный API для тостов. Можно вызывать из любого места (query/router/etc.),
 * не только из React-дерева. Возвращает `id` — передаётся в `toast.dismiss(id)`.
 * `warning` — каноническое имя; `warn` — алиас из §8.3.
 */
export const toast = {
  success: (input: ToastInput | string): string => enqueue('success', input),
  error: (input: ToastInput | string): string => enqueue('error', input),
  warning: (input: ToastInput | string): string => enqueue('warning', input),
  warn: (input: ToastInput | string): string => enqueue('warning', input),
  info: (input: ToastInput | string): string => enqueue('info', input),
  sticky: (input: ToastInput | string): string => enqueue('sticky', input),
  dismiss: (id: string): void => useToastStore.getState().dismiss(id),
  clear: (): void => useToastStore.getState().clear(),
};

/** Hook-обёртка. Возвращает тот же императивный API — для use-в-компонентах стиля. */
export function useToast(): typeof toast {
  return toast;
}
