// LIMITATION: toCsv/fromCsv не экранируют сам разделитель — значения,
// содержащие ',', ломают roundtrip. В рамках FE-TASK-038 применимо только
// к enum-like filter values (ACTIVE, ARCHIVED, SUPPLY, SERVICE, ...).
// Для произвольных строк — нужен RFC 4180 escaping или base64.
const SEP = ',';

export function toCsv(values: readonly string[]): string {
  return values.join(SEP);
}

export function fromCsv(raw: string | null | undefined): string[] {
  if (raw == null || raw === '') return [];
  return raw
    .split(SEP)
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}
