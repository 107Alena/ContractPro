// Эвристика: принадлежит ли риск к отклонениям от политики организации.
// До появления явного поля `source: 'policy'` в OpenAPI (backlog FE-TASK-048+)
// используем substring-match по legal_basis. Extract — чтобы не дрейфовать
// между DeviationsFromPolicy и NextActions.

interface MaybeRisk {
  legal_basis?: string | undefined;
}

export function isPolicyDeviation(risk: MaybeRisk): boolean {
  const basis = (risk.legal_basis ?? '').toLowerCase();
  return basis.includes('политик') || basis.includes('policy');
}
