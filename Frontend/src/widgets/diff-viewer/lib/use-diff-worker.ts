// React-хук для расчёта diff в Web Worker (§11.2 high-architecture).
//
// Поведение:
//  - В браузере: отдельный Worker per-инстанс хука. При смене paragraphs или
//    unmount предыдущий worker terminate-ится — это безопаснее, чем reuse,
//    потому что отменяет уже не нужные тяжёлые вычисления (большой договор
//    может считаться сотни мс).
//  - В jsdom (типичный test env, Worker===undefined): синхронный fallback на
//    computeAllDiffs. Так все тесты UI работают без mock-ов worker.
//  - paragraphs пустой/undefined: result=null, isComputing=false (UI рисует
//    EmptyState).
import { useEffect, useState } from 'react';

import type { ComputedDiffParagraph, DiffParagraph } from '../model/types';
import { computeAllDiffs } from './compute-diff';

interface WorkerOk {
  ok: true;
  result: ComputedDiffParagraph[];
}
interface WorkerErr {
  ok: false;
  error: string;
}
type WorkerResponse = WorkerOk | WorkerErr;

export interface UseDiffWorkerState {
  result: ComputedDiffParagraph[] | null;
  isComputing: boolean;
  error: Error | null;
}

const INITIAL_STATE: UseDiffWorkerState = {
  result: null,
  isComputing: false,
  error: null,
};

export function useDiffWorker(
  paragraphs: readonly DiffParagraph[] | undefined,
): UseDiffWorkerState {
  const [state, setState] = useState<UseDiffWorkerState>(INITIAL_STATE);

  useEffect(() => {
    if (paragraphs === undefined || paragraphs.length === 0) {
      setState(INITIAL_STATE);
      return undefined;
    }

    // Fallback для jsdom / SSR: вычисляем синхронно. Это критично для unit-
    // тестов UI — иначе пришлось бы мокать Worker в каждом тесте.
    if (typeof Worker === 'undefined') {
      try {
        const result = computeAllDiffs(paragraphs);
        setState({ result, isComputing: false, error: null });
      } catch (err) {
        setState({
          result: null,
          isComputing: false,
          error: err instanceof Error ? err : new Error(String(err)),
        });
      }
      return undefined;
    }

    setState({ result: null, isComputing: true, error: null });

    let cancelled = false;
    // import.meta.url + { type: 'module' } — нативный паттерн Vite для бандлинга
    // worker (vite.config.ts manualChunks ловит этот файл по пути и кладёт в
    // chunks/diff-viewer вместе с diff-match-patch).
    const worker = new Worker(new URL('../worker/diff.worker.ts', import.meta.url), {
      type: 'module',
    });

    const onMessage = (event: MessageEvent<WorkerResponse>) => {
      if (cancelled) return;
      const data = event.data;
      if (data.ok) {
        setState({ result: data.result, isComputing: false, error: null });
      } else {
        setState({ result: null, isComputing: false, error: new Error(data.error) });
      }
    };

    const onError = (event: ErrorEvent) => {
      if (cancelled) return;
      setState({
        result: null,
        isComputing: false,
        error: new Error(event.message || 'Diff worker failed'),
      });
    };

    worker.addEventListener('message', onMessage);
    worker.addEventListener('error', onError);
    worker.postMessage({ paragraphs });

    return () => {
      cancelled = true;
      worker.removeEventListener('message', onMessage);
      worker.removeEventListener('error', onError);
      worker.terminate();
    };
  }, [paragraphs]);

  return state;
}
