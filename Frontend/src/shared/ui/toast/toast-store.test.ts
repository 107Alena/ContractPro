import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { __resetToastStoreForTests, TOAST_LIMIT, useToastStore } from './toast-store';
import { toast } from './use-toast';

beforeEach(() => {
  __resetToastStoreForTests();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('toast-store', () => {
  it('enqueues a success toast with default duration', () => {
    const id = toast.success('Saved');
    const { toasts } = useToastStore.getState();
    expect(toasts).toHaveLength(1);
    const item = toasts[0]!;
    expect(item.id).toBe(id);
    expect(item.variant).toBe('success');
    expect(item.title).toBe('Saved');
    expect(item.duration).toBe(5000);
  });

  it('warn is an alias for warning', () => {
    toast.warn('careful');
    const [item] = useToastStore.getState().toasts;
    expect(item?.variant).toBe('warning');
  });

  it('sticky toast has null duration by default', () => {
    toast.sticky({ title: 'keep' });
    const [item] = useToastStore.getState().toasts;
    expect(item?.duration).toBeNull();
  });

  it('custom duration overrides default', () => {
    toast.info({ title: 'x', duration: 1234 });
    expect(useToastStore.getState().toasts[0]?.duration).toBe(1234);
  });

  it('dismiss removes a toast by id', () => {
    const id = toast.success('one');
    toast.dismiss(id);
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it('clear empties the queue', () => {
    toast.success('a');
    toast.error('b');
    toast.clear();
    expect(useToastStore.getState().toasts).toHaveLength(0);
  });

  it(`keeps FIFO limit of ${TOAST_LIMIT} — oldest non-sticky evicted`, () => {
    for (let i = 0; i < TOAST_LIMIT + 2; i += 1) {
      toast.info(`t${i}`);
    }
    const { toasts } = useToastStore.getState();
    expect(toasts).toHaveLength(TOAST_LIMIT);
    expect(toasts[0]?.title).toBe('t2');
    expect(toasts[TOAST_LIMIT - 1]?.title).toBe(`t${TOAST_LIMIT + 1}`);
  });

  it('FIFO prefers to evict non-sticky over sticky', () => {
    toast.sticky('keep');
    for (let i = 0; i < TOAST_LIMIT; i += 1) {
      toast.info(`t${i}`);
    }
    const { toasts } = useToastStore.getState();
    expect(toasts).toHaveLength(TOAST_LIMIT);
    expect(toasts.some((t) => t.title === 'keep')).toBe(true);
    expect(toasts[0]?.title).toBe('keep');
  });

  it('action payload preserved', () => {
    const onClick = vi.fn();
    const id = toast.error({ title: 'fail', action: { label: 'retry', onClick } });
    const [item] = useToastStore.getState().toasts;
    expect(item?.action?.label).toBe('retry');
    item?.action?.onClick(id);
    expect(onClick).toHaveBeenCalledWith(id);
  });

  it('accepts pre-supplied id (for dedup scenarios)', () => {
    toast.info({ id: 'fixed', title: 'x' });
    const [item] = useToastStore.getState().toasts;
    expect(item?.id).toBe('fixed');
  });
});
