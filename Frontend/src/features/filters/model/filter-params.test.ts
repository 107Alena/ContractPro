import { describe, expect, it } from 'vitest';

import { isDefault, parseFilterParams, serializeFilterParams } from './filter-params';
import type { FilterDefinition } from './types';

const STATUS_DEF: FilterDefinition = {
  key: 'status',
  label: 'Статус',
  kind: 'single',
  options: [
    { value: 'ACTIVE', label: 'Активные' },
    { value: 'ARCHIVED', label: 'В архиве' },
  ],
};
const TYPE_DEF: FilterDefinition = {
  key: 'types',
  label: 'Тип договора',
  kind: 'multi',
  options: [
    { value: 'SUPPLY', label: 'Поставка' },
    { value: 'SERVICE', label: 'Услуги' },
  ],
};
const DEFS = [STATUS_DEF, TYPE_DEF];

describe('filter-params', () => {
  it('parseFilterParams: пустой URL → дефолты', () => {
    const result = parseFilterParams(new URLSearchParams(''), DEFS);
    expect(result.status).toBe('');
    expect(result.types).toEqual([]);
  });

  it('parseFilterParams: читает single и CSV', () => {
    const result = parseFilterParams(
      new URLSearchParams('status=ACTIVE&types=SUPPLY,SERVICE'),
      DEFS,
    );
    expect(result.status).toBe('ACTIVE');
    expect(result.types).toEqual(['SUPPLY', 'SERVICE']);
  });

  it('serializeFilterParams: дефолтные значения удаляются', () => {
    const result = serializeFilterParams(new URLSearchParams('foo=bar'), DEFS, {
      status: '',
      types: [],
    });
    expect(result.get('status')).toBeNull();
    expect(result.get('types')).toBeNull();
    expect(result.get('foo')).toBe('bar');
  });

  it('serializeFilterParams: пишет CSV для multi', () => {
    const result = serializeFilterParams(new URLSearchParams(''), DEFS, {
      status: 'ACTIVE',
      types: ['SUPPLY', 'SERVICE'],
    });
    expect(result.get('status')).toBe('ACTIVE');
    expect(result.get('types')).toBe('SUPPLY,SERVICE');
  });

  it('roundtrip: parse(serialize(x)) === x', () => {
    const initial = { status: 'ACTIVE', types: ['SUPPLY'] };
    const serialized = serializeFilterParams(new URLSearchParams(''), DEFS, initial);
    const parsed = parseFilterParams(serialized, DEFS);
    expect(parsed).toEqual(initial);
  });

  it('isDefault: single с пустой строкой — default', () => {
    expect(isDefault(STATUS_DEF, '')).toBe(true);
    expect(isDefault(STATUS_DEF, 'ACTIVE')).toBe(false);
  });

  it('isDefault: multi с пустым массивом — default', () => {
    expect(isDefault(TYPE_DEF, [])).toBe(true);
    expect(isDefault(TYPE_DEF, ['SUPPLY'])).toBe(false);
  });
});
