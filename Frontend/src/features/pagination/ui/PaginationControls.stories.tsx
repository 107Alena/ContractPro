import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { PaginationControls } from './PaginationControls';

const meta = {
  title: 'Features/Pagination/PaginationControls',
  component: PaginationControls,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
  args: {
    page: 1,
    size: 20,
    total: 0,
    onPageChange: () => undefined,
  },
} satisfies Meta<typeof PaginationControls>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({
  initialPage = 1,
  initialSize = 20,
  total = 100,
  isLoading = false,
  isFetching = false,
  withSizeSelect = true,
}: {
  initialPage?: number;
  initialSize?: number;
  total?: number;
  isLoading?: boolean;
  isFetching?: boolean;
  withSizeSelect?: boolean;
}) {
  const [page, setPage] = useState(initialPage);
  const [size, setSize] = useState(initialSize);
  return (
    <div className="w-[640px]">
      <PaginationControls
        page={page}
        size={size}
        total={total}
        onPageChange={setPage}
        {...(withSizeSelect
          ? {
              onSizeChange: (n: number) => {
                setSize(n);
                setPage(1);
              },
            }
          : {})}
        isLoading={isLoading}
        isFetching={isFetching}
      />
    </div>
  );
}

export const Default: Story = {
  render: () => <Harness />,
};

export const Empty: Story = {
  render: () => <Harness total={0} />,
};

export const FirstPage: Story = {
  render: () => <Harness initialPage={1} total={100} />,
};

export const LastPage: Story = {
  render: () => <Harness initialPage={5} total={100} />,
};

export const LargeTotal: Story = {
  render: () => <Harness initialPage={25} total={5000} />,
};

export const Loading: Story = {
  render: () => <Harness isLoading />,
};

export const Fetching: Story = {
  render: () => <Harness isFetching />,
};

export const WithoutSizeSelect: Story = {
  render: () => <Harness withSizeSelect={false} />,
};
