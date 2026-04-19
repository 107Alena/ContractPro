import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { Button } from '@/shared/ui/button';

import { ConfirmDialog } from './confirm-dialog';

const meta = {
  title: 'Shared/ConfirmDialog',
  component: ConfirmDialog,
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
} satisfies Meta<typeof ConfirmDialog>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({
  variant,
  isPending,
  description,
}: {
  variant?: 'danger' | 'primary';
  isPending?: boolean;
  description?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <Button onClick={() => setOpen(true)}>Открыть</Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        onConfirm={() => setOpen(false)}
        title="Удалить договор?"
        description={description ?? 'Действие необратимо'}
        confirmLabel="Удалить"
        cancelLabel="Отмена"
        {...(variant !== undefined && { variant })}
        {...(isPending !== undefined && { isPending })}
      >
        <p className="text-sm">
          Название: <strong>Договор поставки №42</strong>
        </p>
      </ConfirmDialog>
    </div>
  );
}

export const Primary: Story = {
  args: {
    open: false,
    onOpenChange: () => undefined,
    onConfirm: () => undefined,
    title: 'Подтвердить действие',
  },
  render: () => <Harness variant="primary" />,
};

export const Danger: Story = {
  args: {
    open: false,
    onOpenChange: () => undefined,
    onConfirm: () => undefined,
    title: 'Удалить договор?',
  },
  render: () => <Harness variant="danger" />,
};

export const Pending: Story = {
  args: {
    open: false,
    onOpenChange: () => undefined,
    onConfirm: () => undefined,
    title: 'Удаление…',
  },
  render: () => <Harness variant="danger" isPending />,
};
