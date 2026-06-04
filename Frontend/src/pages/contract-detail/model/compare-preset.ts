// buildComparePreset — query-suffix `?base=<prev>&target=<current>` для
// предзаполнения страницы сравнения парой «предыдущая → текущая» версия.
//
// Берём две последние READY-версии по version_number (target — наибольший
// номер, base — следующий). Фильтр по READY: валидный diff существует только
// для проанализированных версий — иначе /compare откроется на NotReady, а не
// populated. Нет пары READY → '' (CTA ведёт на пустой /compare, страница
// честно показывает «Версии не выбраны», как было до Stage 5).
import { type VersionDetails } from '@/entities/version';

export function buildComparePreset(versions: readonly VersionDetails[]): string {
  const ready = versions
    .filter((v) => v.processing_status === 'READY' && Boolean(v.version_id))
    .sort((a, b) => (b.version_number ?? 0) - (a.version_number ?? 0));
  const target = ready[0]?.version_id;
  const base = ready[1]?.version_id;
  if (!base || !target) return '';
  return `?base=${encodeURIComponent(base)}&target=${encodeURIComponent(target)}`;
}
