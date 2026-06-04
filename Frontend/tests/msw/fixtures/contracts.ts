// Фикстуры договоров и версий. Детерминированные даты — 2026-04-15 ±.
// Используются в handlers/contracts.ts, handlers/versions.ts, а также в Storybook.

import type { components } from '@/shared/api/openapi';

import { IDS } from './ids';

type ContractSummary = components['schemas']['ContractSummary'];
type ContractDetails = components['schemas']['ContractDetails'];
type VersionDetails = components['schemas']['VersionDetails'];
type ContractStats = components['schemas']['ContractStats'];

const BASE_DATE = '2026-04-15T10:00:00Z';

export const versionAlphaV1: VersionDetails = {
  version_id: IDS.versions.alphaV1,
  contract_id: IDS.contracts.alpha,
  version_number: 1,
  origin_type: 'UPLOAD',
  origin_description: 'Первичная загрузка',
  parent_version_id: null,
  source_file_name: 'contract-alpha-v1.pdf',
  source_file_size: 524_288,
  processing_status: 'READY',
  processing_status_message: 'Результаты готовы',
  created_at: BASE_DATE,
};

export const versionAlphaV2: VersionDetails = {
  version_id: IDS.versions.alphaV2,
  contract_id: IDS.contracts.alpha,
  version_number: 2,
  origin_type: 'RE_UPLOAD',
  origin_description: 'Новая редакция после замечаний',
  parent_version_id: IDS.versions.alphaV1,
  source_file_name: 'contract-alpha-v2.pdf',
  source_file_size: 576_512,
  processing_status: 'ANALYZING',
  processing_status_message: 'Юридический анализ',
  created_at: '2026-04-16T14:20:00Z',
};

export const versionBetaV1: VersionDetails = {
  version_id: IDS.versions.betaV1,
  contract_id: IDS.contracts.beta,
  version_number: 1,
  origin_type: 'UPLOAD',
  origin_description: null,
  parent_version_id: null,
  source_file_name: 'contract-beta.pdf',
  source_file_size: 204_800,
  processing_status: 'AWAITING_USER_INPUT',
  processing_status_message: 'Требуется подтверждение типа договора',
  created_at: '2026-04-17T09:10:00Z',
};

export const versionGammaV1: VersionDetails = {
  version_id: IDS.versions.gammaV1,
  contract_id: IDS.contracts.gamma,
  version_number: 1,
  origin_type: 'UPLOAD',
  origin_description: null,
  parent_version_id: null,
  source_file_name: 'contract-gamma.pdf',
  source_file_size: 102_400,
  processing_status: 'FAILED',
  processing_status_message: 'Ошибка обработки — PDF повреждён',
  created_at: '2026-04-14T18:45:00Z',
};

// delta — «боевой» READY-договор: текущая версия v2 готова (все CTA активны,
// статус «Готово»), есть предыдущая версия для истории/сравнения.
export const versionDeltaV1: VersionDetails = {
  version_id: IDS.versions.deltaV1,
  contract_id: IDS.contracts.delta,
  version_number: 1,
  origin_type: 'UPLOAD',
  origin_description: 'Первичная загрузка',
  parent_version_id: null,
  source_file_name: 'contract-delta-v1.pdf',
  source_file_size: 312_000,
  processing_status: 'READY',
  processing_status_message: 'Результаты готовы',
  created_at: '2026-04-12T11:00:00Z',
};

export const versionDeltaV2: VersionDetails = {
  version_id: IDS.versions.deltaV2,
  contract_id: IDS.contracts.delta,
  version_number: 2,
  origin_type: 'RE_UPLOAD',
  origin_description: 'Финальная редакция',
  parent_version_id: IDS.versions.deltaV1,
  source_file_name: 'contract-delta-v2.pdf',
  source_file_size: 358_400,
  processing_status: 'READY',
  processing_status_message: 'Результаты готовы',
  created_at: '2026-04-19T16:05:00Z',
};

export const contractDelta: ContractDetails = {
  contract_id: IDS.contracts.delta,
  title: 'Договор подряда на ремонтные работы',
  status: 'ACTIVE',
  current_version: versionDeltaV2,
  created_by_user_id: IDS.users.lawyer,
  created_at: '2026-04-12T11:00:00Z',
  updated_at: '2026-04-19T16:05:00Z',
};

export const contractAlpha: ContractDetails = {
  contract_id: IDS.contracts.alpha,
  title: 'Договор оказания услуг с ООО «Альфа»',
  status: 'ACTIVE',
  current_version: versionAlphaV2,
  created_by_user_id: IDS.users.lawyer,
  created_at: BASE_DATE,
  updated_at: '2026-04-16T14:20:00Z',
};

export const contractBeta: ContractDetails = {
  contract_id: IDS.contracts.beta,
  title: 'Договор поставки оборудования',
  status: 'ACTIVE',
  current_version: versionBetaV1,
  created_by_user_id: IDS.users.lawyer,
  created_at: '2026-04-17T09:10:00Z',
  updated_at: '2026-04-17T09:10:00Z',
};

export const contractGamma: ContractDetails = {
  contract_id: IDS.contracts.gamma,
  title: 'Договор аренды помещения',
  status: 'ARCHIVED',
  current_version: versionGammaV1,
  created_by_user_id: IDS.users.orgAdmin,
  created_at: '2026-04-14T18:45:00Z',
  updated_at: '2026-04-18T08:00:00Z',
};

export const contractSummaries: ContractSummary[] = [
  {
    contract_id: IDS.contracts.alpha,
    title: 'Договор оказания услуг с ООО «Альфа»',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'ANALYZING',
    created_at: BASE_DATE,
    updated_at: '2026-04-16T14:20:00Z',
  },
  {
    contract_id: IDS.contracts.beta,
    title: 'Договор поставки оборудования',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'AWAITING_USER_INPUT',
    created_at: '2026-04-17T09:10:00Z',
    updated_at: '2026-04-17T09:10:00Z',
  },
  {
    contract_id: IDS.contracts.gamma,
    title: 'Договор аренды помещения',
    status: 'ARCHIVED',
    current_version_number: 1,
    processing_status: 'FAILED',
    created_at: '2026-04-14T18:45:00Z',
    updated_at: '2026-04-18T08:00:00Z',
  },
  {
    contract_id: IDS.contracts.delta,
    title: 'Договор подряда на ремонтные работы',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'READY',
    created_at: '2026-04-12T11:00:00Z',
    updated_at: '2026-04-19T16:05:00Z',
  },
];

export const contractDetailsById: Record<string, ContractDetails> = {
  [IDS.contracts.alpha]: contractAlpha,
  [IDS.contracts.beta]: contractBeta,
  [IDS.contracts.gamma]: contractGamma,
  [IDS.contracts.delta]: contractDelta,
};

// Агрегированная статистика GET /contracts/stats (Путь C: мок). Считается из
// contractSummaries в активном скоупе (status=ACTIVE), чтобы оставаться
// консистентной со списком: alpha=ANALYZING, beta=AWAITING_USER_INPUT,
// delta=READY (gamma ARCHIVED — вне дефолтного скоупа). «в работе» = 1 (alpha).
function buildContractStats(): ContractStats {
  const byStatus: ContractStats['by_processing_status'] = {
    uploaded: 0,
    queued: 0,
    processing: 0,
    analyzing: 0,
    awaiting_user_input: 0,
    generating_reports: 0,
    ready: 0,
    partially_failed: 0,
    failed: 0,
    rejected: 0,
    not_started: 0,
  };
  const active = contractSummaries.filter((c) => c.status === 'ACTIVE');
  for (const c of active) {
    const key = (c.processing_status?.toLowerCase() ?? 'not_started') as keyof typeof byStatus;
    if (key in byStatus) byStatus[key] += 1;
    else byStatus.not_started += 1;
  }
  return {
    total: active.length,
    by_processing_status: byStatus,
    // delta (READY) с medium-риском — единственный готовый в активном скоупе.
    by_risk_level: { high: 0, medium: byStatus.ready, low: 0, unknown: 0 },
    updated_at: '2026-04-19T16:10:00Z',
  };
}

export const contractStats: ContractStats = buildContractStats();

export const versionsByContract: Record<string, VersionDetails[]> = {
  [IDS.contracts.alpha]: [versionAlphaV1, versionAlphaV2],
  [IDS.contracts.beta]: [versionBetaV1],
  [IDS.contracts.gamma]: [versionGammaV1],
  [IDS.contracts.delta]: [versionDeltaV1, versionDeltaV2],
};
