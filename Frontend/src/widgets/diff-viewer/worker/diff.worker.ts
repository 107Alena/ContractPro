// Web Worker для вычисления diff (§11.2 high-architecture).
// Главный поток не блокируется на больших договорах. Импорт computeAllDiffs
// тянет за собой diff-match-patch — Vite manualChunks отправит его в
// chunks/diff-viewer, не в главный bundle.
/// <reference lib="webworker" />
import { computeAllDiffs } from '../lib/compute-diff';
import type { DiffParagraph } from '../model/types';

interface DiffRequest {
  paragraphs: readonly DiffParagraph[];
}

self.addEventListener('message', (e: MessageEvent<DiffRequest>) => {
  try {
    const result = computeAllDiffs(e.data.paragraphs);
    (self as unknown as DedicatedWorkerGlobalScope).postMessage({ ok: true, result });
  } catch (err) {
    (self as unknown as DedicatedWorkerGlobalScope).postMessage({
      ok: false,
      error: err instanceof Error ? err.message : String(err),
    });
  }
});
