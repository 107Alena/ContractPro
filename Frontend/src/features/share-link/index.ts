// Barrel: публичный API фичи share-link (§7.6, UR-10, FE-TASK-039).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители —
// widgets/export-share-modal (FE-TASK-039), ReportsPage (FE-TASK-046).
export type { GetShareLinkOptions } from './api/get-share-link';
export { getShareLink, shareLinkEndpoint } from './api/get-share-link';
export type { ShareLinkFormat, ShareLinkInput, ShareLinkResult } from './model/types';
export {
  useShareLink,
  type UseShareLinkOptions,
  type UseShareLinkResult,
} from './model/use-share-link';
