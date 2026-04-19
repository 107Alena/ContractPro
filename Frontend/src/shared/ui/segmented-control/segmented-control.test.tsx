import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  SegmentedControl,
  type SegmentedControlOption,
  segmentedControlVariants,
  segmentedItemVariants,
} from './segmented-control';

afterEach(() => cleanup());

type Status = 'all' | 'active' | 'archived';
const OPTIONS: ReadonlyArray<SegmentedControlOption<Status>> = [
  { value: 'all', label: 'Все' },
  { value: 'active', label: 'Активные' },
  { value: 'archived', label: 'Архив' },
];

describe('segmentedControlVariants', () => {
  it('default size md → h-10', () => {
    expect(segmentedControlVariants({})).toContain('h-10');
  });

  it('size sm → h-8', () => {
    expect(segmentedControlVariants({ size: 'sm' })).toContain('h-8');
  });

  it('fullWidth → w-full', () => {
    expect(segmentedControlVariants({ fullWidth: true })).toContain('w-full');
  });
});

describe('segmentedItemVariants', () => {
  it('has aria-checked styles', () => {
    expect(segmentedItemVariants({})).toContain('aria-checked:bg-bg');
    expect(segmentedItemVariants({})).toContain('aria-checked:shadow-sm');
  });
});

function Controlled({
  initial = 'all',
  disabled = false,
  options = OPTIONS,
}: {
  initial?: Status;
  disabled?: boolean;
  options?: ReadonlyArray<SegmentedControlOption<Status>>;
}) {
  const [value, setValue] = useState<Status>(initial);
  return (
    <SegmentedControl<Status>
      options={options}
      value={value}
      onValueChange={setValue}
      disabled={disabled}
      ariaLabel="Статус"
    />
  );
}

describe('SegmentedControl', () => {
  it('renders role=radiogroup with aria-label', () => {
    render(<Controlled />);
    const group = screen.getByRole('radiogroup', { name: 'Статус' });
    expect(group).toBeInTheDocument();
  });

  it('renders options as role=radio with correct aria-checked', () => {
    render(<Controlled initial="active" />);
    const radios = screen.getAllByRole('radio');
    expect(radios).toHaveLength(3);
    expect(radios[0]).toHaveAttribute('aria-checked', 'false');
    expect(radios[1]).toHaveAttribute('aria-checked', 'true');
    expect(radios[2]).toHaveAttribute('aria-checked', 'false');
  });

  it('click selects option', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    await user.click(screen.getByRole('radio', { name: 'Активные' }));
    expect(onChange).toHaveBeenCalledWith('active');
  });

  it('click on already-selected does not re-emit', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    await user.click(screen.getByRole('radio', { name: 'Все' }));
    expect(onChange).not.toHaveBeenCalled();
  });

  it('keyboard: ArrowRight moves to next and triggers onValueChange', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    screen.getByRole('radio', { name: 'Все' }).focus();
    await user.keyboard('{ArrowRight}');
    expect(onChange).toHaveBeenCalledWith('active');
  });

  it('keyboard: ArrowLeft wraps to last', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    screen.getByRole('radio', { name: 'Все' }).focus();
    await user.keyboard('{ArrowLeft}');
    expect(onChange).toHaveBeenCalledWith('archived');
  });

  it('keyboard: Home → first enabled option', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="active"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    screen.getByRole('radio', { name: 'Активные' }).focus();
    await user.keyboard('{Home}');
    expect(onChange).toHaveBeenLastCalledWith('all');
  });

  it('keyboard: End → last enabled option', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="active"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    screen.getByRole('radio', { name: 'Активные' }).focus();
    await user.keyboard('{End}');
    expect(onChange).toHaveBeenLastCalledWith('archived');
  });

  it('disabled option skipped in keyboard nav', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const options: ReadonlyArray<SegmentedControlOption<Status>> = [
      { value: 'all', label: 'Все' },
      { value: 'active', label: 'Активные', disabled: true },
      { value: 'archived', label: 'Архив' },
    ];
    render(
      <SegmentedControl<Status>
        options={options}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    screen.getByRole('radio', { name: 'Все' }).focus();
    await user.keyboard('{ArrowRight}');
    expect(onChange).toHaveBeenCalledWith('archived');
  });

  it('disabled option is aria-disabled and not clickable', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const options: ReadonlyArray<SegmentedControlOption<Status>> = [
      { value: 'all', label: 'Все' },
      { value: 'active', label: 'Активные', disabled: true },
    ];
    render(
      <SegmentedControl<Status>
        options={options}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    const disabledBtn = screen.getByRole('radio', { name: 'Активные' });
    expect(disabledBtn).toHaveAttribute('aria-disabled', 'true');
    expect(disabledBtn).toBeDisabled();
    await user.click(disabledBtn);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('when disabled, radiogroup has aria-disabled and options inactive', () => {
    render(<Controlled disabled />);
    const group = screen.getByRole('radiogroup');
    expect(group).toHaveAttribute('aria-disabled', 'true');
    screen.getAllByRole('radio').forEach((el) => {
      expect(el).toBeDisabled();
    });
  });

  it('roving tabindex: only selected has tabIndex=0', () => {
    render(<Controlled initial="archived" />);
    const radios = screen.getAllByRole('radio');
    expect(radios[0]).toHaveAttribute('tabindex', '-1');
    expect(radios[1]).toHaveAttribute('tabindex', '-1');
    expect(radios[2]).toHaveAttribute('tabindex', '0');
  });

  it('Space/Enter не меняют value (no-op на уже выбранном)', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <SegmentedControl<Status>
        options={OPTIONS}
        value="all"
        onValueChange={onChange}
        ariaLabel="Статус"
      />,
    );
    screen.getByRole('radio', { name: 'Все' }).focus();
    await user.keyboard(' ');
    await user.keyboard('{Enter}');
    expect(onChange).not.toHaveBeenCalled();
  });
});
