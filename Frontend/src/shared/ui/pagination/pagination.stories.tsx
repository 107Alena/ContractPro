import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { PageSizeSelect, Pagination } from './pagination';

const meta = {
  title: 'Shared/Pagination',
  component: Pagination,
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
  args: {
    page: 1,
    totalPages: 1,
    onPageChange: () => undefined,
  },
} satisfies Meta<typeof Pagination>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({
  totalPages,
  initialPage = 1,
  showPrevNext = true,
  disabled = false,
}: {
  totalPages: number;
  initialPage?: number;
  showPrevNext?: boolean;
  disabled?: boolean;
}) {
  const [page, setPage] = useState(initialPage);
  return (
    <Pagination
      page={page}
      totalPages={totalPages}
      onPageChange={setPage}
      showPrevNext={showPrevNext}
      disabled={disabled}
    />
  );
}

export const Default: Story = {
  render: () => <Harness totalPages={5} />,
};

export const SinglePage: Story = {
  render: () => <Harness totalPages={1} />,
};

export const Many: Story = {
  render: () => <Harness totalPages={50} initialPage={25} />,
};

export const FirstPage: Story = {
  render: () => <Harness totalPages={10} initialPage={1} />,
};

export const LastPage: Story = {
  render: () => <Harness totalPages={10} initialPage={10} />,
};

export const Disabled: Story = {
  render: () => <Harness totalPages={10} initialPage={5} disabled />,
};

export const NoPrevNext: Story = {
  render: () => <Harness totalPages={10} initialPage={4} showPrevNext={false} />,
};

export const WithPageSize: Story = {
  render: () => {
    function HarnessWithSize() {
      const [page, setPage] = useState(2);
      const [size, setSize] = useState(20);
      return (
        <div className="flex flex-col items-center gap-3">
          <Pagination page={page} totalPages={8} onPageChange={setPage} />
          <PageSizeSelect value={size} options={[10, 20, 50, 100]} onChange={setSize} />
        </div>
      );
    }
    return <HarnessWithSize />;
  },
};
