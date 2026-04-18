// @vitest-environment jsdom
//
// ExportShareModal — рендер карточек форматов, RBAC-fallback для BUSINESS_USER
// без export_enabled, клик «Скачать» → navigate(location), клик «Скопировать»
// → clipboard + checkmark, закрытие через «Закрыть».
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { AxiosInstance } from 'axios';
import { type ReactElement } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { __setHttpForTests as setExportHttp } from '@/features/export-download/api/http';
import { __setHttpForTests as setShareHttp } from '@/features/share-link/api/http';
import { sessionStore, type User } from '@/shared/auth/session-store';
import { toast } from '@/shared/ui/toast';

import { ExportShareModal } from './ExportShareModal';

const CONTRACT_ID = 'c0ffee00-1111-2222-3333-444444444444';
const VERSION_ID = 'ba5e0000-aaaa-bbbb-cccc-111111111111';
const PRESIGNED_PDF = 'https://presigned.example/report.pdf?X-Expires=300';
const PRESIGNED_DOCX = 'https://presigned.example/report.docx?X-Expires=300';

function makeUser(
  role: 'LAWYER' | 'BUSINESS_USER' | 'ORG_ADMIN',
  exportEnabled: boolean = false,
): User {
  return {
    user_id: 'u-1',
    email: 'u@test',
    name: 'Test User',
    role,
    organization_id: 'org-1',
    organization_name: 'Test Org',
    permissions: { export_enabled: exportEnabled },
  };
}

type WritableClipboard = { writeText: (text: string) => Promise<void> };
const originalClipboard = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard');

function setClipboard(value: WritableClipboard | undefined): void {
  Object.defineProperty(globalThis.navigator, 'clipboard', {
    value,
    configurable: true,
    writable: true,
  });
}

function restoreClipboard(): void {
  if (originalClipboard) {
    Object.defineProperty(globalThis.navigator, 'clipboard', originalClipboard);
  } else {
    setClipboard(undefined);
  }
}

function makeWrapper(): (props: { children: ReactElement }) => ReactElement {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  function Wrapper({ children }: { children: ReactElement }): ReactElement {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  Wrapper.displayName = 'TestWrapper';
  return Wrapper;
}

let exportGetSpy: ReturnType<typeof vi.fn>;
let shareGetSpy: ReturnType<typeof vi.fn>;
let writeSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  sessionStore.getState().clear();
  exportGetSpy = vi.fn();
  shareGetSpy = vi.fn();
  setExportHttp({ get: exportGetSpy } as unknown as AxiosInstance);
  setShareHttp({ get: shareGetSpy } as unknown as AxiosInstance);
  writeSpy = vi.fn().mockResolvedValue(undefined);
  setClipboard({ writeText: writeSpy as unknown as (t: string) => Promise<void> });
  toast.clear();
});

afterEach(() => {
  setExportHttp(null);
  setShareHttp(null);
  restoreClipboard();
  sessionStore.getState().clear();
  toast.clear();
  cleanup();
  vi.restoreAllMocks();
});

describe('ExportShareModal — RBAC', () => {
  it('LAWYER видит обе карточки форматов (PDF + DOCX)', () => {
    sessionStore.getState().setUser(makeUser('LAWYER'));
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={() => {}}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={() => {}}
        />
      </Wrapper>,
    );

    expect(screen.getByRole('heading', { name: 'PDF' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'DOCX' })).toBeInTheDocument();
    expect(screen.getByTestId('export-download-pdf')).toBeInTheDocument();
    expect(screen.getByTestId('export-share-docx')).toBeInTheDocument();
  });

  it('BUSINESS_USER без export_enabled — показывается fallback-сообщение', () => {
    sessionStore.getState().setUser(makeUser('BUSINESS_USER', false));
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={() => {}}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={() => {}}
        />
      </Wrapper>,
    );

    expect(screen.queryByTestId('export-download-pdf')).not.toBeInTheDocument();
    expect(screen.getByText(/нет прав на экспорт/i)).toBeInTheDocument();
  });

  it('BUSINESS_USER c export_enabled=true — видит карточки', () => {
    sessionStore.getState().setUser(makeUser('BUSINESS_USER', true));
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={() => {}}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={() => {}}
        />
      </Wrapper>,
    );

    expect(screen.getByTestId('export-download-pdf')).toBeInTheDocument();
  });
});

describe('ExportShareModal — download flow', () => {
  it('клик «Скачать» на PDF → navigate вызван с presigned URL', async () => {
    sessionStore.getState().setUser(makeUser('LAWYER'));
    exportGetSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED_PDF },
    });
    const navigate = vi.fn();
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={() => {}}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={navigate}
        />
      </Wrapper>,
    );

    fireEvent.click(screen.getByTestId('export-download-pdf'));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith(PRESIGNED_PDF));
  });

  it('клик «Скачать» на DOCX → navigate c другим URL', async () => {
    sessionStore.getState().setUser(makeUser('ORG_ADMIN'));
    exportGetSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED_DOCX },
    });
    const navigate = vi.fn();
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={() => {}}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={navigate}
        />
      </Wrapper>,
    );

    fireEvent.click(screen.getByTestId('export-download-docx'));
    await waitFor(() => expect(navigate).toHaveBeenCalledWith(PRESIGNED_DOCX));
  });
});

describe('ExportShareModal — share flow', () => {
  it('клик «Скопировать ссылку» → clipboard.writeText + checkmark', async () => {
    sessionStore.getState().setUser(makeUser('LAWYER'));
    shareGetSpy.mockResolvedValueOnce({
      status: 302,
      headers: { location: PRESIGNED_PDF },
    });
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={() => {}}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={() => {}}
        />
      </Wrapper>,
    );

    fireEvent.click(screen.getByTestId('export-share-pdf'));
    await waitFor(() => expect(writeSpy).toHaveBeenCalledWith(PRESIGNED_PDF));
    await screen.findByText(/Ссылка скопирована/i);
  });
});

describe('ExportShareModal — close', () => {
  it('клик «Закрыть» вызывает onOpenChange(false)', () => {
    sessionStore.getState().setUser(makeUser('LAWYER'));
    const onOpenChange = vi.fn();
    const Wrapper = makeWrapper();
    render(
      <Wrapper>
        <ExportShareModal
          open
          onOpenChange={onOpenChange}
          contractId={CONTRACT_ID}
          versionId={VERSION_ID}
          navigate={() => {}}
        />
      </Wrapper>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Закрыть' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
