// FeedbackBlock — виджет «Полезен ли результат?» на ResultPage
// (UR-11, §17.4 экран 5). Композирует feature feedback-submit: два CTA
// (да/нет) + опциональное текстовое поле комментария.
//
// RBAC: эндпоинт `/feedback` доступен всем аутентифицированным — виджет
// рендерится для всех ролей. Клиент не управляет invalidation очередями
// (write-only endpoint, §17.3).
import { useCallback, useState } from 'react';

import { useFeedbackSubmit } from '@/features/feedback-submit';
import { Button } from '@/shared/ui/button';
import { toast } from '@/shared/ui/toast';

export interface FeedbackBlockProps {
  contractId: string;
  versionId: string;
}

export function FeedbackBlock({ contractId, versionId }: FeedbackBlockProps): JSX.Element {
  const [choice, setChoice] = useState<'useful' | 'not-useful' | null>(null);
  const [comment, setComment] = useState('');
  const [submitted, setSubmitted] = useState(false);

  const mutation = useFeedbackSubmit({
    onSuccess: () => {
      setSubmitted(true);
      toast.success({ title: 'Спасибо за обратную связь' });
    },
    onError: (_err, message) => {
      toast.error({
        title: message.title,
        ...(message.hint ? { description: message.hint } : {}),
      });
    },
  });

  const chooseUseful = useCallback(() => {
    setChoice('useful');
    mutation.submit({ contractId, versionId, isUseful: true });
  }, [contractId, versionId, mutation]);

  const chooseNotUseful = useCallback(() => {
    setChoice('not-useful');
  }, []);

  const sendWithComment = useCallback(() => {
    mutation.submit({
      contractId,
      versionId,
      isUseful: false,
      ...(comment.trim() ? { comment: comment.trim() } : {}),
    });
  }, [contractId, versionId, comment, mutation]);

  const isUsefulSubmitting = mutation.isPending && choice === 'useful';
  const isNegativeSubmitting = mutation.isPending && choice === 'not-useful';

  if (submitted) {
    return (
      <section
        aria-label="Обратная связь"
        data-testid="feedback-block-done"
        className="flex flex-col gap-2 rounded-md border border-success/30 bg-bg p-5 shadow-sm"
      >
        <h2 className="text-sm font-semibold uppercase tracking-wide text-success">
          Обратная связь принята
        </h2>
        <p className="text-sm text-fg-muted">
          Мы используем ваш отзыв для улучшения качества проверок.
        </p>
      </section>
    );
  }

  return (
    <section
      aria-label="Обратная связь"
      data-testid="feedback-block"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Результат был полезен?
        </h2>
        <p className="text-xs text-fg-muted">Ваш ответ помогает нам улучшать алгоритмы анализа.</p>
      </header>

      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="primary"
          onClick={chooseUseful}
          loading={isUsefulSubmitting}
          disabled={mutation.isPending}
          data-testid="feedback-useful"
        >
          Да, полезен
        </Button>
        <Button
          type="button"
          variant="secondary"
          onClick={chooseNotUseful}
          disabled={mutation.isPending}
          data-testid="feedback-not-useful"
        >
          Нет
        </Button>
      </div>

      {choice === 'not-useful' ? (
        <div className="flex flex-col gap-2" data-testid="feedback-comment">
          <label htmlFor="feedback-block-comment" className="text-xs font-medium text-fg-muted">
            Что можно улучшить?
          </label>
          <textarea
            id="feedback-block-comment"
            value={comment}
            onChange={(event) => setComment(event.target.value)}
            rows={3}
            placeholder="Короткий комментарий (не обязательно)"
            className="rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
          />
          <Button
            type="button"
            variant="primary"
            onClick={sendWithComment}
            loading={isNegativeSubmitting}
            disabled={mutation.isPending}
            data-testid="feedback-send"
          >
            Отправить
          </Button>
        </div>
      ) : null}
    </section>
  );
}
