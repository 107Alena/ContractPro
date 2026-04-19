// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type {
  VersionDiffStructuralChange,
  VersionDiffTextChange,
} from '@/features/comparison-start';

import { ChangesTable } from './changes-table';

afterEach(cleanup);

const TEXT_CHANGES: readonly VersionDiffTextChange[] = [
  { type: 'added', path: 'section.1/clause.1', new_text: 'Новый пункт 1.1.' },
  { type: 'removed', path: 'section.2/clause.4', old_text: 'Удалённый пункт 2.4.' },
  { type: 'modified', path: 'section.3/clause.2', old_text: 'A', new_text: 'B' },
];

const STRUCTURAL_CHANGES: readonly VersionDiffStructuralChange[] = [
  { type: 'moved', node_id: 'section.4' },
  { type: 'added', node_id: 'section.6' },
];

describe('ChangesTable', () => {
  it('рендерит все строки при filter=all', () => {
    render(<ChangesTable changes={TEXT_CHANGES} structuralChanges={STRUCTURAL_CHANGES} />);
    expect(screen.getByTestId('changes-table')).toBeTruthy();
    // 3 текстовых + 2 структурных = 5
    expect(screen.getAllByText(/section\./).length).toBeGreaterThanOrEqual(5);
  });

  it('фильтрует только текстовые изменения', () => {
    render(
      <ChangesTable
        changes={TEXT_CHANGES}
        structuralChanges={STRUCTURAL_CHANGES}
        filter="textual"
      />,
    );
    expect(screen.queryByText('section.4')).toBeNull();
    expect(screen.queryByText('section.6')).toBeNull();
    expect(screen.getByText('section.1/clause.1')).toBeTruthy();
  });

  it('фильтрует только структурные', () => {
    render(
      <ChangesTable
        changes={TEXT_CHANGES}
        structuralChanges={STRUCTURAL_CHANGES}
        filter="structural"
      />,
    );
    expect(screen.queryByText('section.1/clause.1')).toBeNull();
    expect(screen.getByText('section.4')).toBeTruthy();
    expect(screen.getByText('section.6')).toBeTruthy();
  });

  it('high-risk оставляет только removed', () => {
    render(
      <ChangesTable
        changes={TEXT_CHANGES}
        structuralChanges={STRUCTURAL_CHANGES}
        filter="high-risk"
      />,
    );
    expect(screen.getByText('section.2/clause.4')).toBeTruthy();
    expect(screen.queryByText('section.1/clause.1')).toBeNull();
    expect(screen.queryByText('section.6')).toBeNull();
  });

  it('показывает empty-state, если после фильтрации нет строк', () => {
    render(<ChangesTable changes={[]} filter="all" />);
    expect(screen.getByText('Нет изменений по выбранному фильтру')).toBeTruthy();
  });

  it('показывает loading-state при isLoading', () => {
    render(<ChangesTable changes={TEXT_CHANGES} isLoading />);
    expect(screen.getByText('Загрузка изменений…')).toBeTruthy();
  });
});
