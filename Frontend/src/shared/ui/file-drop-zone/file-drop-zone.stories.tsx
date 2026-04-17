import type { Meta, StoryObj } from '@storybook/react';
import { useRef } from 'react';

import { Button } from '@/shared/ui/button';

import { FileDropZone, type FileDropZoneHandle } from './file-drop-zone';

const meta = {
  title: 'Shared/FileDropZone',
  component: FileDropZone,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
} satisfies Meta<typeof FileDropZone>;
export default meta;

type Story = StoryObj<typeof meta>;

function makeFile(name: string, size: number, type = 'application/pdf'): File {
  const file = new File(['stub'], name, { type });
  Object.defineProperty(file, 'size', { value: size });
  return file;
}

export const Default: Story = {
  args: {
    onAccepted: (f) => console.warn('accepted:', f.name),
    onError: (e) => console.warn('error:', e.code),
  },
};

export const Selected: Story = {
  args: {
    defaultFile: makeFile('Договор-аренды-2026.pdf', 1024 * 540),
  },
};

export const Loading: Story = {
  args: {
    loading: true,
  },
};

export const Disabled: Story = {
  args: {
    disabled: true,
  },
};

export const ErrorTooLarge: Story = {
  name: 'Error: FILE_TOO_LARGE',
  render: () => {
    function Demo() {
      const ref = useRef<FileDropZoneHandle>(null);
      return (
        <div className="space-y-3">
          <FileDropZone
            ref={ref}
            // Маленький лимит, чтобы любой файл провалил проверку.
            maxSize={1}
            idleHint="Лимит 1 байт — попробуйте перетащить любой файл, чтобы увидеть ошибку."
          />
          <Button variant="ghost" size="sm" onClick={() => ref.current?.reset()}>
            Сбросить
          </Button>
        </div>
      );
    }
    return <Demo />;
  },
};

export const ErrorUnsupportedFormat: Story = {
  name: 'Error: UNSUPPORTED_FORMAT (без флага FEATURE_DOCX_UPLOAD)',
  args: {
    idleHint: 'По умолчанию активен только PDF. Перетащите .docx — увидите ошибку.',
  },
};

export const WithDocxFlag: Story = {
  name: 'FEATURE_DOCX_UPLOAD = true',
  args: {
    featureFlags: { FEATURE_DOCX_UPLOAD: true },
    idleHint: 'Активны PDF, DOCX, DOC.',
  },
};

export const CustomIdleText: Story = {
  args: {
    idleTitle: 'Загрузите проект договора для проверки',
    idleHint: 'PDF до 20 МБ. Содержимое распознаётся OCR при необходимости.',
  },
};
