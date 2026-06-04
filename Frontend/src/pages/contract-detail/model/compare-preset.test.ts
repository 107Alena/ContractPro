import { describe, expect, it } from 'vitest';

import { type VersionDetails } from '@/entities/version';

import { buildComparePreset } from './compare-preset';

function v(
  n: number,
  processing_status: VersionDetails['processing_status'] = 'READY',
  version_id = `v${n}`,
): VersionDetails {
  return { version_id, version_number: n, processing_status };
}

describe('buildComparePreset', () => {
  it('две READY → base=предыдущая, target=текущая', () => {
    expect(buildComparePreset([v(1), v(2)])).toBe('?base=v1&target=v2');
  });

  it('сортирует по version_number (порядок входа не важен)', () => {
    expect(buildComparePreset([v(2), v(1), v(3)])).toBe('?base=v2&target=v3');
  });

  it('< 2 READY → пусто (CTA ведёт на пустой /compare)', () => {
    expect(buildComparePreset([v(1)])).toBe('');
    expect(buildComparePreset([])).toBe('');
  });

  it('пропускает не-READY версии — берёт пару из READY', () => {
    expect(buildComparePreset([v(1), v(2), v(3, 'PROCESSING')])).toBe('?base=v1&target=v2');
  });

  it('нет пары READY (current ещё анализируется) → пусто', () => {
    expect(buildComparePreset([v(1), v(2, 'PROCESSING')])).toBe('');
  });
});
