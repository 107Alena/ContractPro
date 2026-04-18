// useCopy — React-хук копирования строки в буфер обмена (§8.3 high-architecture).
//
// Основной путь — Clipboard API (`navigator.clipboard.writeText`).
// Fallback — textarea + document.execCommand('copy') для старых браузеров
// и тестового окружения (jsdom).
//
// Возвращает `{ copy, copied }`. `copied` переключается в true на resetMs
// миллисекунд после успешного копирования — для UI-checkmark'а.
import { useCallback, useEffect, useRef, useState } from 'react';

const DEFAULT_RESET_MS = 1500;

async function writeWithExecCommand(text: string): Promise<boolean> {
  if (typeof document === 'undefined') return false;
  const textarea = document.createElement('textarea');
  textarea.value = text;
  // Скрываем textarea от скринридеров и визуального рендера — элемент живёт
  // ровно на время execCommand'а.
  textarea.setAttribute('readonly', '');
  textarea.setAttribute('aria-hidden', 'true');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  textarea.style.pointerEvents = 'none';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    document.body.removeChild(textarea);
  }
}

export interface UseCopyResult {
  /** Копирует строку в clipboard. Возвращает true при успехе. */
  copy: (text: string) => Promise<boolean>;
  /** true в течение resetMs после последнего успешного копирования. */
  copied: boolean;
}

export function useCopy(resetMs: number = DEFAULT_RESET_MS): UseCopyResult {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    };
  }, []);

  const copy = useCallback(
    async (text: string): Promise<boolean> => {
      let ok = false;
      const clip = typeof navigator !== 'undefined' ? navigator.clipboard : undefined;
      if (clip && typeof clip.writeText === 'function') {
        try {
          await clip.writeText(text);
          ok = true;
        } catch {
          ok = await writeWithExecCommand(text);
        }
      } else {
        ok = await writeWithExecCommand(text);
      }
      if (ok) {
        setCopied(true);
        if (timerRef.current !== null) clearTimeout(timerRef.current);
        timerRef.current = setTimeout(() => setCopied(false), resetMs);
      }
      return ok;
    },
    [resetMs],
  );

  return { copy, copied };
}
