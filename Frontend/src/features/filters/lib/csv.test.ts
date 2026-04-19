import { describe, expect, it } from 'vitest';

import { fromCsv, toCsv } from './csv';

describe('filters/lib/csv', () => {
  it('toCsv: собирает значения через запятую', () => {
    expect(toCsv(['a', 'b', 'c'])).toBe('a,b,c');
  });

  it('toCsv: пустой массив → пустая строка', () => {
    expect(toCsv([])).toBe('');
  });

  it('fromCsv: парсит CSV строку', () => {
    expect(fromCsv('a,b,c')).toEqual(['a', 'b', 'c']);
  });

  it('fromCsv: null/undefined/пустая → []', () => {
    expect(fromCsv(null)).toEqual([]);
    expect(fromCsv(undefined)).toEqual([]);
    expect(fromCsv('')).toEqual([]);
  });

  it('fromCsv: обрезает пробелы и выкидывает пустые сегменты', () => {
    expect(fromCsv(' a , ,b, ')).toEqual(['a', 'b']);
  });

  it('roundtrip: fromCsv(toCsv(xs)) === xs', () => {
    expect(fromCsv(toCsv(['ACTIVE', 'ARCHIVED']))).toEqual(['ACTIVE', 'ARCHIVED']);
  });
});
