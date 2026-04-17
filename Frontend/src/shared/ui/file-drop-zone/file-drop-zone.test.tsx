// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileDropZone, type FileDropZoneHandle } from './file-drop-zone';

const PDF_HEADER = [0x25, 0x50, 0x44, 0x46];
const RANDOM_HEADER = [0xde, 0xad, 0xbe, 0xef];

function makeFile(bytes: number[], type: string, name = 'doc'): File {
  return new File([new Uint8Array(bytes)], name, { type });
}

describe('FileDropZone — рендер', () => {
  afterEach(cleanup);

  it('idle: показывает заголовок, hint с активными форматами и кнопку', () => {
    render(<FileDropZone />);
    expect(screen.getByText('Перетащите файл сюда')).toBeTruthy();
    expect(screen.getByText(/PDF/)).toBeTruthy();
    expect(screen.getByText(/20 МБ/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Выбрать файл' })).toBeTruthy();
  });

  it('selected: показывает имя файла, размер и кнопку «Удалить»', () => {
    const file = makeFile(PDF_HEADER, 'application/pdf', 'doc.pdf');
    Object.defineProperty(file, 'size', { value: 540 * 1024 });
    render(<FileDropZone defaultFile={file} />);
    expect(screen.getByText('doc.pdf')).toBeTruthy();
    expect(screen.getByText('540 КБ')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Удалить' })).toBeTruthy();
  });

  it('loading: показывает «Проверяем файл…» и data-state=loading', () => {
    const { container } = render(<FileDropZone loading />);
    expect(screen.getByText('Проверяем файл…')).toBeTruthy();
    expect(container.querySelector('[data-state="loading"]')).toBeTruthy();
  });

  it('disabled: data-state=disabled, aria-disabled, кнопка disabled', () => {
    const { container } = render(<FileDropZone disabled />);
    const root = container.querySelector('[data-state="disabled"]');
    expect(root).toBeTruthy();
    expect(root?.getAttribute('aria-disabled')).toBe('true');
    expect(screen.getByRole('button', { name: 'Выбрать файл' }).hasAttribute('disabled')).toBe(
      true,
    );
  });

  it('idleTitle и idleHint переопределяются props-ами', () => {
    render(<FileDropZone idleTitle="Загрузите договор" idleHint="Только PDF" />);
    expect(screen.getByText('Загрузите договор')).toBeTruthy();
    expect(screen.getByText('Только PDF')).toBeTruthy();
  });
});

describe('FileDropZone — взаимодействие', () => {
  afterEach(cleanup);

  it('onAccepted вызывается при валидном PDF', async () => {
    const onAccepted = vi.fn();
    const file = makeFile([...PDF_HEADER, 0, 0, 0, 0, 0, 0, 0, 0], 'application/pdf', 'a.pdf');
    const { container } = render(<FileDropZone onAccepted={onAccepted} />);
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });
    await vi.waitFor(() => expect(onAccepted).toHaveBeenCalledTimes(1));
    expect(onAccepted.mock.calls[0]?.[0]).toBe(file);
  });

  it('onError с UNSUPPORTED_FORMAT для неподдерживаемого MIME', async () => {
    const onError = vi.fn();
    const file = makeFile([0, 0, 0, 0], 'image/png', 'a.png');
    const { container } = render(<FileDropZone onError={onError} />);
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });
    await vi.waitFor(() => expect(onError).toHaveBeenCalled());
    expect(onError.mock.calls[0]?.[0].code).toBe('UNSUPPORTED_FORMAT');
    expect(screen.getByRole('alert').textContent).toMatch(/Поддерживается/);
  });

  it('onError с INVALID_FILE при подмене расширения', async () => {
    const onError = vi.fn();
    const file = makeFile(
      [...RANDOM_HEADER, 0, 0, 0, 0, 0, 0, 0, 0],
      'application/pdf',
      'fake.pdf',
    );
    const { container } = render(<FileDropZone onError={onError} />);
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });
    await vi.waitFor(() => expect(onError).toHaveBeenCalled());
    expect(onError.mock.calls[0]?.[0].code).toBe('INVALID_FILE');
  });

  it('onError с FILE_TOO_LARGE при превышении лимита', async () => {
    const onError = vi.fn();
    const file = makeFile([...PDF_HEADER, 0, 0, 0, 0, 0, 0, 0, 0], 'application/pdf', 'big.pdf');
    Object.defineProperty(file, 'size', { value: 11 });
    const { container } = render(<FileDropZone maxSize={10} onError={onError} />);
    const input = container.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });
    await vi.waitFor(() => expect(onError).toHaveBeenCalled());
    expect(onError.mock.calls[0]?.[0].code).toBe('FILE_TOO_LARGE');
  });

  it('кнопка «Удалить» сбрасывает файл и вызывает onReset', () => {
    const onReset = vi.fn();
    const file = makeFile(PDF_HEADER, 'application/pdf', 'doc.pdf');
    render(<FileDropZone defaultFile={file} onReset={onReset} />);
    fireEvent.click(screen.getByRole('button', { name: 'Удалить' }));
    expect(onReset).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('doc.pdf')).toBeNull();
  });

  it('ref.reset() сбрасывает файл и зовёт onReset', () => {
    const onReset = vi.fn();
    const ref = createRef<FileDropZoneHandle>();
    const file = makeFile(PDF_HEADER, 'application/pdf', 'doc.pdf');
    render(<FileDropZone ref={ref} defaultFile={file} onReset={onReset} />);
    expect(screen.getByText('doc.pdf')).toBeTruthy();
    ref.current?.reset();
    expect(onReset).toHaveBeenCalledTimes(1);
  });

  it('ref.reset() на пустом state не зовёт onReset (silent)', () => {
    const onReset = vi.fn();
    const ref = createRef<FileDropZoneHandle>();
    render(<FileDropZone ref={ref} onReset={onReset} />);
    ref.current?.reset();
    expect(onReset).not.toHaveBeenCalled();
  });
});

describe('FileDropZone — feature flags', () => {
  afterEach(cleanup);

  it('hint показывает только PDF без FEATURE_DOCX_UPLOAD', () => {
    render(<FileDropZone />);
    expect(screen.getByText(/Поддерживается: PDF\./)).toBeTruthy();
  });

  it('hint показывает PDF, DOCX, DOC при FEATURE_DOCX_UPLOAD=true', () => {
    render(<FileDropZone featureFlags={{ FEATURE_DOCX_UPLOAD: true }} />);
    expect(screen.getByText(/Поддерживается: PDF, DOCX, DOC\./)).toBeTruthy();
  });
});
