// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import PDFNavigator from './pdf-navigator';

afterEach(cleanup);

describe('PDFNavigator (stub)', () => {
  it('рендерит версию и имя файла, показывает заглушку', () => {
    render(<PDFNavigator versionId="v42" sourceFileName="alpha-v2.pdf" />);
    expect(screen.getByTestId('pdf-navigator')).toBeDefined();
    expect(screen.getByText(/v42/)).toBeDefined();
    expect(screen.getByText(/alpha-v2\.pdf/)).toBeDefined();
    expect(screen.getByText(/PDF-просмотр станет доступен/)).toBeDefined();
  });

  it('onClose срабатывает по клику на «Скрыть»', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<PDFNavigator versionId="v42" onClose={onClose} />);
    await user.click(screen.getByTestId('pdf-navigator-close'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
