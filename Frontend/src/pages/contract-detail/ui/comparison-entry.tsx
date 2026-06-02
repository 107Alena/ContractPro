// ComparisonEntry — секция «Сравнение версий» карточки договора
// (Figma 306:2 → Comparison Entry 315:2/315:4). Honest-shell: вердикт сравнения
// («12 различий», «стало лучше», дельта профиля риска) требует отдельного
// /compare-вызова и недоступен здесь → не выдумываем. Показываем приглашение
// открыть полноценное сравнение (или подсказку загрузить версию, если её одна).
import { Link } from 'react-router-dom';

import { buttonVariants, Card } from '@/shared/ui';

export interface ComparisonEntryProps {
  contractId: string;
  versionCount: number;
}

export function ComparisonEntry({ contractId, versionCount }: ComparisonEntryProps): JSX.Element {
  const canCompare = versionCount >= 2;
  return (
    <Card
      as="section"
      aria-label="Сравнение версий"
      radius="xl"
      className="flex flex-col gap-4 border border-border-subtle px-7 py-6 shadow-none"
    >
      <h2 className="text-18 font-semibold text-fg">Сравнение версий</h2>
      {canCompare ? (
        <>
          <p className="text-14 leading-5 text-fg-muted">
            Сравните текущую версию с предыдущей: что изменилось в тексте и как поменялся профиль
            риска.
          </p>
          <Link
            to={`/contracts/${contractId}/compare`}
            className={`${buttonVariants({ variant: 'secondary', size: 'md' })} self-start`}
          >
            Открыть сравнение версий
          </Link>
        </>
      ) : (
        <p className="text-14 leading-5 text-fg-muted">
          Чтобы сравнить версии, загрузите новую редакцию договора — появится разбор изменений.
        </p>
      )}
    </Card>
  );
}
