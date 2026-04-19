import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { Button } from '@/shared/ui/button';

import { ConfirmDeleteContractModal } from './ConfirmDeleteContractModal';

const meta = {
  title: 'Features/ContractDelete/ConfirmDeleteContractModal',
  component: ConfirmDeleteContractModal,
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
} satisfies Meta<typeof ConfirmDeleteContractModal>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({ isPending, contractTitle }: { isPending?: boolean; contractTitle?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <Button variant="danger" onClick={() => setOpen(true)}>
        Удалить договор
      </Button>
      <ConfirmDeleteContractModal
        open={open}
        onOpenChange={setOpen}
        onConfirm={() => setOpen(false)}
        {...(contractTitle !== undefined && { contractTitle })}
        {...(isPending !== undefined && { isPending })}
      />
    </div>
  );
}

export const Default: Story = {
  args: {
    open: false,
    onOpenChange: () => undefined,
    onConfirm: () => undefined,
  },
  render: () => <Harness contractTitle="Договор поставки №42" />,
};

export const WithoutTitle: Story = {
  args: {
    open: false,
    onOpenChange: () => undefined,
    onConfirm: () => undefined,
  },
  render: () => <Harness />,
};

export const Pending: Story = {
  args: {
    open: false,
    onOpenChange: () => undefined,
    onConfirm: () => undefined,
  },
  render: () => <Harness contractTitle="Договор поставки №42" isPending />,
};
