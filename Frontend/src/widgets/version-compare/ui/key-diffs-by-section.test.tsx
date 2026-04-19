// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { KeyDiffsBySection } from './key-diffs-by-section';

afterEach(cleanup);

describe('KeyDiffsBySection', () => {
  it('рендерит empty-state, если sections пустой', () => {
    render(<KeyDiffsBySection sections={[]} />);
    expect(screen.getByTestId('key-diffs-empty')).toBeTruthy();
    expect(screen.getByText('Нет данных по разделам')).toBeTruthy();
  });

  it('рендерит секции с бейджами +X / -Y / ~Z', () => {
    render(
      <KeyDiffsBySection
        sections={[
          { section: 'section.2', added: 3, removed: 1, modified: 2 },
          { section: 'section.5', added: 0, removed: 4, modified: 1 },
        ]}
      />,
    );
    expect(screen.getByTestId('key-diffs-row-section.2')).toBeTruthy();
    expect(screen.getByTestId('key-diffs-row-section.5')).toBeTruthy();
    const addedBadges = screen.getAllByTestId('key-diffs-added');
    expect(addedBadges).toHaveLength(2);
    expect(addedBadges[0]?.textContent).toBe('+3');
    expect(addedBadges[1]?.textContent).toBe('+0');
  });
});
