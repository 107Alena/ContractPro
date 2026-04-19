import { describe, expect, it } from 'vitest';

import { getVisibleWindow } from './window-virtualization';

describe('getVisibleWindow', () => {
  const items = Array.from({ length: 100 }, (_, i) => i);

  it('пустой массив → start=0,end=0,offsetY=0', () => {
    expect(getVisibleWindow([], 0, 480, 40, 5)).toEqual({ start: 0, end: 0, offsetY: 0 });
  });

  it('scrollTop=0 рендерит первое окно с overscan вниз', () => {
    // viewport=480/rowHeight=40 → 12 видимых строк. overscan=5 вниз.
    // Вверху overscan не уменьшает start (Math.max(0, ...)).
    const w = getVisibleWindow(items, 0, 480, 40, 5);
    expect(w.start).toBe(0);
    expect(w.end).toBe(17); // 12 + 5
    expect(w.offsetY).toBe(0);
  });

  it('scrollTop в середине — окно сдвинуто, offsetY = start*rowHeight', () => {
    const w = getVisibleWindow(items, 800, 480, 40, 5);
    // firstVisible = 20, lastVisible = ceil((800+480)/40)=32
    // start = max(0, 20-5)=15, end = min(100, 32+5)=37
    expect(w.start).toBe(15);
    expect(w.end).toBe(37);
    expect(w.offsetY).toBe(15 * 40);
  });

  it('end clamps к items.length у конца списка', () => {
    // total height = 100*40 = 4000. scrollTop=3700 → last viewport.
    const w = getVisibleWindow(items, 3700, 480, 40, 5);
    expect(w.end).toBe(100);
    expect(w.start).toBeLessThan(100);
  });

  it('rowHeight=0 деградирует в "рендерим всё"', () => {
    expect(getVisibleWindow(items, 0, 480, 0, 5)).toEqual({ start: 0, end: 100, offsetY: 0 });
  });

  it('отрицательный scrollTop (iOS rubber-band) трактуется как 0', () => {
    const w = getVisibleWindow(items, -120, 480, 40, 5);
    expect(w.start).toBe(0);
    expect(w.offsetY).toBe(0);
  });

  it('overscan=0 не добавляет запас', () => {
    const w = getVisibleWindow(items, 800, 480, 40, 0);
    expect(w.start).toBe(20);
    expect(w.end).toBe(32);
  });
});
