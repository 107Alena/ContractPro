// Фикстура сравнения версий (VersionDiff).

import type { components } from '@/shared/api/openapi';

import { IDS } from './ids';

type VersionDiff = components['schemas']['VersionDiff'];

export const versionDiffAlpha: VersionDiff = {
  base_version_id: IDS.versions.alphaV1,
  target_version_id: IDS.versions.alphaV2,
  text_diff_count: 4,
  structural_diff_count: 2,
  text_diffs: [
    {
      type: 'modified',
      path: '/sections/4/clauses/2/text',
      old_text: 'Исполнитель несёт ответственность за нарушение сроков.',
      new_text:
        'Исполнитель несёт ответственность в размере 0,1% от стоимости услуг за каждый день просрочки, но не более 10% от общей стоимости.',
    },
    {
      type: 'added',
      path: '/sections/9/clauses/1/text',
      old_text: null,
      new_text: 'Споры разрешаются путём переговоров в течение 30 дней.',
    },
    {
      type: 'removed',
      path: '/sections/7/clauses/1/text',
      old_text: 'Заказчик вправе в любой момент отказаться от договора.',
      new_text: null,
    },
    {
      type: 'modified',
      path: '/sections/1/title',
      old_text: 'Предмет договора',
      new_text: 'Предмет договора и объём услуг',
    },
  ],
  structural_diffs: [
    {
      type: 'added',
      node_id: 'appendix-3',
      old_value: null,
      new_value: {},
    },
    {
      type: 'modified',
      node_id: 'section-4',
      old_value: {},
      new_value: {},
    },
  ],
};
