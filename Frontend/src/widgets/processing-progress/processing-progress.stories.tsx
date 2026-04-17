import type { Meta, StoryObj } from '@storybook/react';

import { Button } from '@/shared/ui/button';

import { ProcessingProgress } from './processing-progress';

const meta = {
  title: 'Widgets/ProcessingProgress',
  component: ProcessingProgress,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <div className="max-w-xl">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ProcessingProgress>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Uploaded: Story = { args: { status: 'UPLOADED' } };
export const Queued: Story = { args: { status: 'QUEUED' } };
export const Processing: Story = { args: { status: 'PROCESSING' } };
export const Analyzing: Story = { args: { status: 'ANALYZING' } };
export const AwaitingUserInput: Story = {
  name: 'AWAITING_USER_INPUT (без CTA)',
  args: { status: 'AWAITING_USER_INPUT' },
};
export const AwaitingUserInputWithAction: Story = {
  name: 'AWAITING_USER_INPUT (с inline CTA)',
  args: {
    status: 'AWAITING_USER_INPUT',
    awaitingAction: (
      <Button size="sm" onClick={() => console.warn('open confirm-type modal')}>
        Подтвердить тип договора
      </Button>
    ),
  },
};
export const GeneratingReports: Story = { args: { status: 'GENERATING_REPORTS' } };
export const Ready: Story = { args: { status: 'READY' } };
export const PartiallyFailed: Story = {
  args: {
    status: 'PARTIALLY_FAILED',
    errorMessage: 'Отчёт по рекомендациям не сформирован. correlation_id: abcd-1234',
  },
};
export const Failed: Story = {
  args: {
    status: 'FAILED',
    errorMessage: 'Не удалось извлечь текст из PDF. correlation_id: efgh-5678',
  },
};
export const FailedOnReports: Story = {
  name: 'FAILED на шаге GENERATING_REPORTS (errorAtStep override)',
  args: {
    status: 'FAILED',
    errorAtStep: 'GENERATING_REPORTS',
    errorMessage: 'Сбой Reporting Engine. correlation_id: ijkl-9012',
  },
};
export const Rejected: Story = {
  args: {
    status: 'REJECTED',
    errorMessage: 'MIME-тип не поддерживается: application/zip',
  },
};
export const LongLabelOverflow: Story = {
  name: 'Edge-case: ограниченная ширина контейнера',
  render: (args) => (
    <div className="max-w-xs rounded-lg border border-dashed border-border p-2">
      <ProcessingProgress {...args} />
    </div>
  ),
  args: {
    status: 'GENERATING_REPORTS',
  },
};
