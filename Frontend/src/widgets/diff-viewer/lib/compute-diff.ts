// Pure-функции для вычисления diff одного и многих параграфов.
// Изолировано от React, чтобы тот же модуль работал и в Web Worker
// (worker/diff.worker.ts), и синхронно в jsdom-тестах (lib/use-diff-worker).
//
// Зависимость на diff-match-patch локализована здесь — UI-компоненты её НЕ
// импортируют. Vite manualChunks (vite.config.ts) кладёт diff-match-patch в
// chunks/diff-viewer, а главный chunk страницы остаётся маленьким.
import DiffMatchPatch from 'diff-match-patch';

import type { ComputedDiffParagraph, DiffParagraph, DiffSegment } from '../model/types';

// diff-match-patch — сравнительно тяжёлый ad-hoc state, лениво создаём один
// инстанс на модуль. Это безопасно: API чистое (diff_main + diff_cleanupSemantic
// не имеют persistent state между вызовами).
let dmpSingleton: DiffMatchPatch | null = null;
function getDmp(): DiffMatchPatch {
  if (dmpSingleton === null) {
    dmpSingleton = new DiffMatchPatch();
  }
  return dmpSingleton;
}

// diff-match-patch использует -1/0/1 для DELETE/EQUAL/INSERT. Маппим в наш enum.
function dmpOpToKind(op: number): DiffSegment['kind'] {
  if (op === -1) return 'delete';
  if (op === 1) return 'insert';
  return 'equal';
}

/**
 * Считает diff между двумя текстами одного параграфа.
 * Использует diff_cleanupSemantic для человеко-читаемых блоков (§11.2).
 *
 * Edge-case: если оба текста пустые — возвращает [] (не один пустой equal).
 */
export function computeParagraphDiff(oldText: string, newText: string): DiffSegment[] {
  if (oldText === '' && newText === '') return [];
  const dmp = getDmp();
  const diffs = dmp.diff_main(oldText, newText);
  dmp.diff_cleanupSemantic(diffs);
  return diffs.map(
    (tuple: [number, string]): DiffSegment => ({
      kind: dmpOpToKind(tuple[0]),
      text: tuple[1],
    }),
  );
}

/**
 * Считает diff для всех параграфов batch-ом. status==='unchanged' идёт по
 * fast-path без обращения к diff-match-patch — экономит CPU при больших
 * договорах, где меняется ~5-10% параграфов.
 */
export function computeAllDiffs(paragraphs: readonly DiffParagraph[]): ComputedDiffParagraph[] {
  const result: ComputedDiffParagraph[] = [];
  for (const paragraph of paragraphs) {
    if (paragraph.status === 'unchanged') {
      result.push({
        paragraph,
        segments: [{ kind: 'equal', text: paragraph.baseText }],
      });
      continue;
    }
    result.push({
      paragraph,
      segments: computeParagraphDiff(paragraph.baseText, paragraph.targetText),
    });
  }
  return result;
}
