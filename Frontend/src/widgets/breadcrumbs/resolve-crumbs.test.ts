import type { UIMatch } from 'react-router-dom';
import { describe, expect, it } from 'vitest';

import { resolveCrumbs } from './resolve-crumbs';

function match(
  id: string,
  pathname: string,
  handle: unknown = undefined,
  params: Record<string, string | undefined> = {},
): UIMatch {
  return {
    id,
    pathname,
    params,
    data: undefined,
    handle,
  } as unknown as UIMatch;
}

describe('resolveCrumbs', () => {
  it('пропускает matches без handle.crumb', () => {
    const result = resolveCrumbs([
      match('root', '/', undefined),
      match('layout', '/', { something: 'else' }),
      match('dashboard', '/dashboard', { crumb: 'Главная' }),
    ]);
    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({ id: 'dashboard', label: 'Главная', current: true });
    expect(result[0]).not.toHaveProperty('href');
  });

  it('последний crumb помечается current=true и не имеет href', () => {
    const result = resolveCrumbs([
      match('c', '/contracts', { crumb: 'Документы' }),
      match('cd', '/contracts/42', { crumb: 'Договор 42' }, { id: '42' }),
    ]);
    expect(result).toHaveLength(2);
    expect(result[0]).toMatchObject({ label: 'Документы', href: '/contracts', current: false });
    expect(result[1]).toMatchObject({ label: 'Договор 42', current: true });
    expect(result[1]).not.toHaveProperty('href');
  });

  it('разрешает функциональный crumb с match', () => {
    const result = resolveCrumbs([
      match(
        'cd',
        '/contracts/abc',
        {
          crumb: (m: UIMatch) => `Договор ${(m.params as { id?: string }).id ?? '???'}`,
        },
        { id: 'abc' },
      ),
    ]);
    expect(result[0]?.label).toBe('Договор abc');
  });

  it('пустой ввод → пустой массив', () => {
    expect(resolveCrumbs([])).toEqual([]);
  });

  it('все matches без handle → пустой массив', () => {
    const result = resolveCrumbs([
      match('a', '/a', null),
      match('b', '/b', {}),
      match('c', '/c', { crumb: 123 }),
    ]);
    expect(result).toEqual([]);
  });
});
