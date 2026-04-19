import { describe, expect, it } from 'vitest';

import type { DiffParagraph } from '../model/types';
import { computeAllDiffs, computeParagraphDiff } from './compute-diff';

describe('computeParagraphDiff', () => {
  it('возвращает [] для двух пустых строк (fast-path edge-case)', () => {
    expect(computeParagraphDiff('', '')).toEqual([]);
  });

  it('идентичные тексты → один equal-сегмент', () => {
    const result = computeParagraphDiff('Стороны заключили договор.', 'Стороны заключили договор.');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ kind: 'equal', text: 'Стороны заключили договор.' });
  });

  it('insertion → equal + insert', () => {
    const result = computeParagraphDiff('Срок: 30 дней.', 'Срок: 30 рабочих дней.');
    expect(result.some((s) => s.kind === 'insert')).toBe(true);
    // Объединение всех сегментов с учётом операции должно реконструировать новый текст.
    const reconstructed = result
      .filter((s) => s.kind !== 'delete')
      .map((s) => s.text)
      .join('');
    expect(reconstructed).toBe('Срок: 30 рабочих дней.');
  });

  it('deletion → equal + delete', () => {
    const result = computeParagraphDiff(
      'Стороны: ООО "Альфа" и ООО "Бета".',
      'Стороны: ООО "Альфа".',
    );
    expect(result.some((s) => s.kind === 'delete')).toBe(true);
    const reconstructed = result
      .filter((s) => s.kind !== 'insert')
      .map((s) => s.text)
      .join('');
    expect(reconstructed).toBe('Стороны: ООО "Альфа" и ООО "Бета".');
  });

  it('modification: complete replace', () => {
    const result = computeParagraphDiff('aaaa', 'bbbb');
    expect(result.some((s) => s.kind === 'delete')).toBe(true);
    expect(result.some((s) => s.kind === 'insert')).toBe(true);
  });

  it('добавление с пустого: только insert', () => {
    const result = computeParagraphDiff('', 'Новый параграф.');
    expect(result).toEqual([{ kind: 'insert', text: 'Новый параграф.' }]);
  });

  it('удаление до пустого: только delete', () => {
    const result = computeParagraphDiff('Старый параграф.', '');
    expect(result).toEqual([{ kind: 'delete', text: 'Старый параграф.' }]);
  });
});

describe('computeAllDiffs', () => {
  it('пустой массив → пустой массив', () => {
    expect(computeAllDiffs([])).toEqual([]);
  });

  it('status=unchanged идёт по fast-path: один equal без вызова dmp', () => {
    const para: DiffParagraph = {
      id: 'p1',
      baseText: 'Без изменений.',
      targetText: 'Без изменений.',
      status: 'unchanged',
    };
    const result = computeAllDiffs([para]);
    expect(result).toHaveLength(1);
    expect(result[0]?.segments).toEqual([{ kind: 'equal', text: 'Без изменений.' }]);
    expect(result[0]?.paragraph).toBe(para);
  });

  it('смешанные статусы обрабатываются независимо', () => {
    const paragraphs: DiffParagraph[] = [
      { id: 'p1', baseText: 'A', targetText: 'A', status: 'unchanged' },
      { id: 'p2', baseText: 'B', targetText: 'C', status: 'modified' },
      { id: 'p3', baseText: '', targetText: 'D', status: 'added' },
    ];
    const result = computeAllDiffs(paragraphs);
    expect(result).toHaveLength(3);
    expect(result[0]?.paragraph.id).toBe('p1');
    expect(result[1]?.paragraph.id).toBe('p2');
    expect(result[2]?.paragraph.id).toBe('p3');
    expect(result[2]?.segments).toEqual([{ kind: 'insert', text: 'D' }]);
  });
});
