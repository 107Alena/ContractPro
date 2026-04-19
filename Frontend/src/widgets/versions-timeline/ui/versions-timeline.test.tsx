// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { ChecksHistory } from './checks-history';
import { VersionsTimeline } from './versions-timeline';

const VERSIONS = [
  {
    version_id: 'v1',
    version_number: 1,
    processing_status: 'READY' as const,
    origin_type: 'UPLOAD' as const,
    source_file_name: 'alpha-v1.pdf',
    created_at: '2026-04-15T10:00:00Z',
  },
  {
    version_id: 'v2',
    version_number: 2,
    processing_status: 'ANALYZING' as const,
    origin_type: 'RE_UPLOAD' as const,
    source_file_name: 'alpha-v2.pdf',
    created_at: '2026-04-16T14:20:00Z',
  },
];

function wrap(ui: JSX.Element): JSX.Element {
  return <MemoryRouter>{ui}</MemoryRouter>;
}

afterEach(cleanup);

describe('VersionsTimeline', () => {
  it('empty — без версий показывает плейсхолдер', () => {
    render(wrap(<VersionsTimeline contractId="c1" versions={[]} />));
    expect(screen.getByText(/Версий пока нет/i)).toBeDefined();
  });

  it('loading — рендерит spinner', () => {
    render(wrap(<VersionsTimeline contractId="c1" versions={undefined} isLoading />));
    expect(screen.getByTestId('versions-timeline-loading')).toBeDefined();
  });

  it('error — рендерит role=alert', () => {
    render(
      wrap(<VersionsTimeline contractId="c1" versions={undefined} error={new Error('boom')} />),
    );
    expect(screen.getByRole('alert')).toBeDefined();
  });

  it('рендерит элементы, отсортированные по version_number DESC', () => {
    render(wrap(<VersionsTimeline contractId="c1" versions={VERSIONS} />));
    const items = screen.getAllByTestId('versions-timeline-item');
    expect(items).toHaveLength(2);
    // Первый элемент в DOM — v2 (DESC сортировка).
    expect(within(items[0] as HTMLElement).getByText('v2')).toBeDefined();
    expect(within(items[1] as HTMLElement).getByText('v1')).toBeDefined();
  });
});

describe('ChecksHistory', () => {
  it('empty — без версий показывает плейсхолдер', () => {
    render(wrap(<ChecksHistory contractId="c1" versions={[]} />));
    expect(screen.getByText(/Проверок пока не было/i)).toBeDefined();
  });

  it('loading — рендерит spinner', () => {
    render(wrap(<ChecksHistory contractId="c1" versions={undefined} isLoading />));
    expect(screen.getByTestId('checks-history-loading')).toBeDefined();
  });

  it('error — рендерит role=alert', () => {
    render(wrap(<ChecksHistory contractId="c1" versions={undefined} error={new Error('boom')} />));
    expect(screen.getByRole('alert')).toBeDefined();
  });

  it('рендерит строки таблицы с ссылками на версии', () => {
    render(wrap(<ChecksHistory contractId="c1" versions={VERSIONS} />));
    const rows = screen.getAllByTestId('checks-history-row');
    expect(rows).toHaveLength(2);
    const openLinks = screen.getAllByRole('link', { name: /Открыть/ });
    expect(openLinks.length).toBeGreaterThanOrEqual(2);
  });
});
