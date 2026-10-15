import type { FormatsResponse } from './api';

/**
 * normalizeFormat hand-mirrors `internal/convert/convert.go` `NormalizeFormat`
 * (held equal by `TestFormatAliasesMatchFrontend`): trim whitespace, strip one
 * leading `.`, lowercase, then fold the same alias table (`jpeg->jpg`,
 * `tif->tiff`, `htm->html`, `tex->latex`, `stp->step`, `igs->iges`), identity
 * for everything else. The two tables MUST be kept in sync — a
 * client that instead matches raw file extensions against pair sources would
 * wrongly reject `photo.jpeg` and `paper.tex` even though the server accepts
 * both via this exact alias.
 */
export function normalizeFormat(raw: string): string {
  const trimmed = raw.trim();
  const withoutDot = trimmed.startsWith('.') ? trimmed.slice(1) : trimmed;
  const lower = withoutDot.toLowerCase();

  switch (lower) {
    case 'jpeg':
      return 'jpg';
    case 'tif':
      return 'tiff';
    case 'htm':
      return 'html';
    case 'tex':
      return 'latex';
    case 'stp':
      return 'step';
    case 'igs':
      return 'iges';
    default:
      return lower;
  }
}

/**
 * sourceFormatOfFilename derives the normalized source format from a
 * filename's extension (the substring after the LAST `.`). Returns `''` when
 * there is no dot, when the only dot is a leading dotfile marker (e.g.
 * `.gitignore`), or when the trailing segment after the last dot is empty
 * (e.g. `archive.`).
 */
export function sourceFormatOfFilename(filename: string): string {
  const lastDot = filename.lastIndexOf('.');
  if (lastDot <= 0 || lastDot === filename.length - 1) {
    return '';
  }
  return normalizeFormat(filename.slice(lastDot + 1));
}

/**
 * targetFormatsFor returns the deduped, alphabetically sorted list of target
 * formats reachable from a given source format across every engine class in
 * `resp`, so the selector only ever offers conversions the server actually
 * supports for the file the user picked (this is F3's fix: the previous
 * global-union selector could default to an engine-incompatible target).
 */
export function targetFormatsFor(resp: FormatsResponse, source: string): string[] {
  const normalizedSource = normalizeFormat(source);
  if (!normalizedSource) {
    return [];
  }

  const targets = new Set<string>();
  for (const engine of Object.values(resp.engines)) {
    for (const pair of engine.pairs) {
      if (normalizeFormat(pair[0]) === normalizedSource) {
        targets.add(normalizeFormat(pair[1]));
      }
    }
  }
  return Array.from(targets).sort();
}

/**
 * targetFormatsOf returns the deduped, alphabetically sorted union of every
 * engine class's target formats (pair[1]), so all engine classes reported by
 * GET /v1/formats are reachable from a single select box. This is now only
 * the fallback used before a file is chosen — once a file is selected,
 * `targetFormatsFor` narrows the list to that file's source format.
 */
export function targetFormatsOf(resp: FormatsResponse): string[] {
  const targets = new Set<string>();
  for (const engine of Object.values(resp.engines)) {
    for (const pair of engine.pairs) {
      targets.add(pair[1]);
    }
  }
  return Array.from(targets).sort();
}

export type OperationRow = { operation: string; engine: string; from: string; to: string };

const operationRowKeys = ['operation', 'engine', 'from', 'to'] as const;

function compareOperationRows(a: OperationRow, b: OperationRow): number {
  for (const key of operationRowKeys) {
    if (a[key] !== b[key]) {
      return a[key] < b[key] ? -1 : 1;
    }
  }
  return 0;
}

export function operationTable(resp: FormatsResponse): OperationRow[] {
  const tree = resp.operations ?? { convert: resp.engines };
  const rows: OperationRow[] = [];
  for (const [operation, engines] of Object.entries(tree)) {
    for (const [engine, formats] of Object.entries(engines)) {
      for (const [from, to] of formats.pairs) {
        rows.push({ operation, engine, from, to });
      }
    }
  }
  return rows.sort(compareOperationRows);
}

export function operationsOf(resp: FormatsResponse): string[] {
  return Array.from(new Set(operationTable(resp).map((row) => row.operation))).sort();
}

export function targetFormatsForOperation(resp: FormatsResponse, operation: string, source: string): string[] {
  const normalizedSource = normalizeFormat(source);
  if (!normalizedSource) {
    return [];
  }

  const targets = new Set<string>();
  for (const row of operationTable(resp)) {
    if (row.operation === operation && normalizeFormat(row.from) === normalizedSource) {
      targets.add(normalizeFormat(row.to));
    }
  }
  return Array.from(targets).sort();
}
