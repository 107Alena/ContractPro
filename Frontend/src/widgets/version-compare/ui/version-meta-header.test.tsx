// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { VersionMetaHeader } from './version-meta-header';

afterEach(cleanup);

describe('VersionMetaHeader', () => {
  it('рендерит обе колонки и плейсхолдеры, если versions undefined', () => {
    render(<VersionMetaHeader />);
    expect(screen.getByTestId('version-meta-header')).toBeTruthy();
    const placeholders = screen.getAllByText('Версия не выбрана');
    expect(placeholders).toHaveLength(2);
  });

  it('показывает version_number, title, author и дату в локали ru-RU', () => {
    render(
      <VersionMetaHeader
        base={{
          versionId: 'b1',
          versionNumber: 1,
          title: 'Договор поставки',
          authorName: 'Иванов И.И.',
          createdAt: '2026-01-15T10:00:00Z',
        }}
        target={{
          versionId: 'b2',
          versionNumber: 2,
          title: 'Договор поставки v2',
          authorName: 'Петров П.П.',
          createdAt: '2026-04-19T10:00:00Z',
        }}
      />,
    );
    expect(screen.getByText('v1')).toBeTruthy();
    expect(screen.getByText('v2')).toBeTruthy();
    expect(screen.getByText('Иванов И.И.')).toBeTruthy();
    expect(screen.getByText('Петров П.П.')).toBeTruthy();
    // Локаль ru-RU включает «января», «апреля» в long-формате.
    expect(screen.getByText(/января|январь/)).toBeTruthy();
  });

  it('игнорирует невалидный ISO в createdAt без падения', () => {
    render(<VersionMetaHeader base={{ versionId: 'b1', createdAt: 'not-a-date' }} />);
    expect(screen.getByTestId('version-meta-header-base')).toBeTruthy();
  });
});
