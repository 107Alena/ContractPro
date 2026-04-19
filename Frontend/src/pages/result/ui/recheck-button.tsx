// RecheckButton — кнопка «Проверить заново» в header ResultPage.
// Доступна LAWYER + ORG_ADMIN (RBAC 'version.recheck', §5.5). Обёртывает
// feature version-recheck: вызывает POST /contracts/{id}/versions/{vid}/recheck,
// показывает toast на успех/ошибку. Invalidation versions+status —
// внутри useRecheckVersion.
import { useRecheckVersion } from '@/features/version-recheck';
import { useCan } from '@/shared/auth/rbac';
import { Button } from '@/shared/ui/button';
import { toast } from '@/shared/ui/toast';

export interface RecheckButtonProps {
  contractId: string;
  versionId: string;
  /** Отключает кнопку, если ре-чек не применим (REJECTED, UPLOADED etc). */
  disabled?: boolean | undefined;
  /** Визуальный вариант — primary для FAILED-экрана, secondary для READY. */
  variant?: 'primary' | 'secondary' | undefined;
}

export function RecheckButton({
  contractId,
  versionId,
  disabled,
  variant = 'secondary',
}: RecheckButtonProps): JSX.Element | null {
  const canRecheck = useCan('version.recheck');
  const mutation = useRecheckVersion({
    onSuccess: () => {
      toast.success({ title: 'Повторная проверка запущена' });
    },
    onError: (_err, message) => {
      toast.error({
        title: message.title,
        ...(message.hint ? { description: message.hint } : {}),
      });
    },
  });

  if (!canRecheck) return null;

  return (
    <Button
      type="button"
      variant={variant}
      loading={mutation.isPending}
      disabled={disabled === true || mutation.isPending}
      onClick={() => mutation.recheck({ contractId, versionId })}
      data-testid="recheck-button"
    >
      Проверить заново
    </Button>
  );
}
