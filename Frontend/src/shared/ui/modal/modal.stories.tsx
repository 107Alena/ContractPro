import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from '@storybook/test';
import { useState } from 'react';

import { Button } from '@/shared/ui/button';

import {
  Modal,
  ModalBody,
  ModalClose,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalHeader,
  ModalTitle,
  ModalTrigger,
} from './modal';

const meta = {
  title: 'Shared/Modal',
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

function Sample({ size = 'md' }: { size?: 'sm' | 'md' | 'lg' }) {
  return (
    <Modal>
      <ModalTrigger asChild>
        <Button variant="primary">Открыть модалку</Button>
      </ModalTrigger>
      <ModalContent size={size}>
        <ModalHeader>
          <ModalTitle>Подтверждение действия</ModalTitle>
          <ModalDescription>Проверьте параметры перед отправкой.</ModalDescription>
        </ModalHeader>
        <ModalBody>
          Договор будет отправлен на проверку. Процесс может занять до двух минут.
        </ModalBody>
        <ModalFooter>
          <ModalClose asChild>
            <Button variant="secondary">Отмена</Button>
          </ModalClose>
          <ModalClose asChild>
            <Button variant="primary">Продолжить</Button>
          </ModalClose>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}

export const Default: Story = { render: () => <Sample /> };

export const SizeSmall: Story = {
  name: 'Size: sm',
  render: () => <Sample size="sm" />,
};

export const SizeLarge: Story = {
  name: 'Size: lg',
  render: () => <Sample size="lg" />,
};

export const DefaultOpen: Story = {
  name: 'defaultOpen (для a11y-snapshot)',
  render: () => (
    <Modal defaultOpen>
      <ModalTrigger asChild>
        <Button variant="primary">Триггер</Button>
      </ModalTrigger>
      <ModalContent>
        <ModalHeader>
          <ModalTitle>Пример для axe</ModalTitle>
          <ModalDescription>Модалка открыта сразу — axe успевает проверить DOM.</ModalDescription>
        </ModalHeader>
        <ModalBody>Контент для a11y-сканирования.</ModalBody>
        <ModalFooter>
          <ModalClose asChild>
            <Button variant="secondary">Закрыть</Button>
          </ModalClose>
        </ModalFooter>
      </ModalContent>
    </Modal>
  ),
};

export const DisableDismiss: Story = {
  name: 'Blocking (без closeOnOverlay / без ESC)',
  render: () => (
    <Modal defaultOpen>
      <ModalTrigger asChild>
        <Button variant="primary">Триггер</Button>
      </ModalTrigger>
      <ModalContent dismissOnOverlay={false} disableEscape>
        <ModalHeader>
          <ModalTitle>Критичная операция</ModalTitle>
          <ModalDescription>
            Невозможно закрыть случайным кликом. Только явное действие.
          </ModalDescription>
        </ModalHeader>
        <ModalBody>Подтвердите удаление версии.</ModalBody>
        <ModalFooter>
          <ModalClose asChild>
            <Button variant="danger">Удалить</Button>
          </ModalClose>
          <ModalClose asChild>
            <Button variant="secondary">Отмена</Button>
          </ModalClose>
        </ModalFooter>
      </ModalContent>
    </Modal>
  ),
};

export const Controlled: Story = {
  render: () => {
    function Wrapper() {
      const [open, setOpen] = useState(false);
      return (
        <div className="flex flex-col items-center gap-3">
          <Button onClick={() => setOpen(true)}>Controlled open</Button>
          <Modal open={open} onOpenChange={setOpen}>
            <ModalContent>
              <ModalHeader>
                <ModalTitle>Controlled</ModalTitle>
              </ModalHeader>
              <ModalBody>open={String(open)}</ModalBody>
              <ModalFooter>
                <Button variant="secondary" onClick={() => setOpen(false)}>
                  Закрыть
                </Button>
              </ModalFooter>
            </ModalContent>
          </Modal>
        </div>
      );
    }
    return <Wrapper />;
  },
};

export const KeyboardEscape: Story = {
  name: 'A11y: ESC закрывает модалку',
  render: () => <Sample />,
  play: async ({ canvasElement, step }) => {
    const canvas = within(canvasElement);
    const trigger = canvas.getByRole('button', { name: 'Открыть модалку' });
    await step('Открываем модалку', async () => {
      await userEvent.click(trigger);
    });
    await step('ESC закрывает', async () => {
      await userEvent.keyboard('{Escape}');
      // После закрытия title исчезает.
      await expect(document.querySelector('[role="dialog"]')).toBeNull();
    });
  },
};
