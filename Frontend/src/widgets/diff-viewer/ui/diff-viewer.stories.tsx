import type { Meta, StoryObj } from '@storybook/react';

import type { DiffParagraph } from '../model/types';
import { DiffViewer } from './diff-viewer';

const SAMPLE: readonly DiffParagraph[] = [
  {
    id: 'p1',
    section: '1. Предмет договора',
    baseText:
      'Поставщик обязуется передать в собственность Покупателя товар, наименование, ассортимент и количество которого определяются в Спецификации.',
    targetText:
      'Поставщик обязуется передать в собственность Покупателя товар надлежащего качества, наименование, ассортимент и количество которого определяются в Спецификации (Приложение №1).',
    status: 'modified',
  },
  {
    id: 'p2',
    section: '2. Цена и порядок расчётов',
    baseText: 'Цена товара составляет 1 200 000 (один миллион двести тысяч) рублей.',
    targetText: 'Цена товара составляет 1 350 000 (один миллион триста пятьдесят тысяч) рублей.',
    status: 'modified',
  },
  {
    id: 'p3',
    section: '2. Цена и порядок расчётов',
    baseText: '',
    targetText:
      'Покупатель производит предоплату в размере 30% в течение 5 (пяти) рабочих дней с момента подписания настоящего Договора.',
    status: 'added',
  },
  {
    id: 'p4',
    section: '3. Срок поставки',
    baseText:
      'Срок поставки товара — 30 (тридцать) календарных дней с момента подписания Договора.',
    targetText:
      'Срок поставки товара — 30 (тридцать) рабочих дней с момента поступления предоплаты на расчётный счёт Поставщика.',
    status: 'modified',
  },
  {
    id: 'p5',
    section: '4. Ответственность сторон',
    baseText:
      'За нарушение сроков поставки Поставщик уплачивает неустойку в размере 0,1% от стоимости непоставленного товара за каждый день просрочки.',
    targetText: '',
    status: 'removed',
  },
  {
    id: 'p6',
    section: '5. Заключительные положения',
    baseText:
      'Все споры по настоящему Договору решаются путём переговоров. При недостижении согласия — в Арбитражном суде г. Москвы.',
    targetText:
      'Все споры по настоящему Договору решаются путём переговоров. При недостижении согласия — в Арбитражном суде г. Москвы.',
    status: 'unchanged',
  },
];

const ADDED_ONLY: readonly DiffParagraph[] = [
  {
    id: 'a1',
    baseText: '',
    targetText: 'Стороны согласовали добавление пункта 4.7 о форс-мажорных обстоятельствах.',
    status: 'added',
  },
  {
    id: 'a2',
    baseText: '',
    targetText:
      'К форс-мажору относятся: стихийные бедствия, военные действия, акты органов государственной власти.',
    status: 'added',
  },
];

const REMOVED_ONLY: readonly DiffParagraph[] = [
  {
    id: 'r1',
    baseText: 'Старая редакция п. 6.2 о праве одностороннего расторжения договора Покупателем.',
    targetText: '',
    status: 'removed',
  },
  {
    id: 'r2',
    baseText: 'Уведомление о расторжении направляется не позднее, чем за 30 дней.',
    targetText: '',
    status: 'removed',
  },
];

function generateMany(): readonly DiffParagraph[] {
  const sections = [
    '1. Предмет',
    '2. Цена',
    '3. Сроки',
    '4. Ответственность',
    '5. Прочее',
  ] as const;
  const items: DiffParagraph[] = [];
  for (let i = 0; i < 80; i += 1) {
    const section: string = sections[i % sections.length] ?? 'Прочее';
    const mod = i % 4;
    if (mod === 0) {
      items.push({
        id: `m-${i}`,
        section,
        baseText: `Параграф ${i}: оригинальный текст условия договора с подробным описанием обязательств сторон.`,
        targetText: `Параграф ${i}: уточнённый текст условия договора с подробным описанием обязательств сторон и сроков.`,
        status: 'modified',
      });
    } else if (mod === 1) {
      items.push({
        id: `m-${i}`,
        section,
        baseText: '',
        targetText: `Параграф ${i}: новое положение, добавленное в редакции от ${i + 1}.04.2026.`,
        status: 'added',
      });
    } else if (mod === 2) {
      items.push({
        id: `m-${i}`,
        section,
        baseText: `Параграф ${i}: устаревшее положение, исключённое из новой редакции.`,
        targetText: '',
        status: 'removed',
      });
    } else {
      items.push({
        id: `m-${i}`,
        section,
        baseText: `Параграф ${i}: положение без изменений между версиями.`,
        targetText: `Параграф ${i}: положение без изменений между версиями.`,
        status: 'unchanged',
      });
    }
  }
  return items;
}

const meta = {
  title: 'Widgets/DiffViewer',
  component: DiffViewer,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <div className="max-w-5xl">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof DiffViewer>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  name: 'SideBySide (default)',
  args: { paragraphs: SAMPLE },
};

export const Inline: Story = {
  args: { paragraphs: SAMPLE, mode: 'inline' },
};

export const Loading: Story = {
  args: { paragraphs: SAMPLE, isLoading: true },
};

export const ErrorState: Story = {
  args: {
    paragraphs: SAMPLE,
    error: new Error('comparison_job_failed: correlation_id=abcd-1234'),
    onRetry: () => {
      console.warn('retry comparison job');
    },
  },
};

export const Empty: Story = {
  args: { paragraphs: [] },
};

export const ManyParagraphs: Story = {
  name: 'ManyParagraphs (~80, демонстрирует виртуализацию)',
  args: { paragraphs: generateMany(), viewportHeight: 600 },
};

export const HighlightAdded: Story = {
  name: 'Только добавления',
  args: { paragraphs: ADDED_ONLY },
};

export const HighlightRemoved: Story = {
  name: 'Только удаления',
  args: { paragraphs: REMOVED_ONLY },
};

export const MixedChanges: Story = {
  args: { paragraphs: SAMPLE, mode: 'inline' },
};
