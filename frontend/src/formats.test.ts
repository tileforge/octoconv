import { describe, expect, it } from 'vitest';
import {
  normalizeFormat,
  operationTable,
  operationsOf,
  sourceFormatOfFilename,
  targetFormatsFor,
  targetFormatsForOperation,
  targetFormatsOf,
} from './formats';
import type { FormatsResponse } from './api';

describe('normalizeFormat', () => {
  it('folds jpeg to jpg, case-insensitively and whitespace/dot-tolerant', () => {
    expect(normalizeFormat('JPEG')).toBe('jpg');
    expect(normalizeFormat(' .JPEG ')).toBe('jpg');
    expect(normalizeFormat('jpeg')).toBe('jpg');
  });

  it('folds the rest of the alias table', () => {
    expect(normalizeFormat('tif')).toBe('tiff');
    expect(normalizeFormat('htm')).toBe('html');
    expect(normalizeFormat('tex')).toBe('latex');
    expect(normalizeFormat('stp')).toBe('step');
    expect(normalizeFormat('igs')).toBe('iges');
  });

  it('is identity for unaliased formats', () => {
    expect(normalizeFormat('md')).toBe('md');
    expect(normalizeFormat('docx')).toBe('docx');
  });

  it('returns empty string for empty input', () => {
    expect(normalizeFormat('')).toBe('');
  });
});

describe('sourceFormatOfFilename', () => {
  it('derives the normalized source format from the last extension', () => {
    expect(sourceFormatOfFilename('photo.jpeg')).toBe('jpg');
    expect(sourceFormatOfFilename('paper.tex')).toBe('latex');
    expect(sourceFormatOfFilename('page.HTM')).toBe('html');
    expect(sourceFormatOfFilename('scan.TIF')).toBe('tiff');
    expect(sourceFormatOfFilename('part.STP')).toBe('step');
    expect(sourceFormatOfFilename('part.igs')).toBe('iges');
  });

  it('uses the last dot when the filename has multiple dots', () => {
    expect(sourceFormatOfFilename('my.report.docx')).toBe('docx');
  });

  it('returns empty string when there is no usable extension', () => {
    expect(sourceFormatOfFilename('README')).toBe('');
    expect(sourceFormatOfFilename('.gitignore')).toBe('');
    expect(sourceFormatOfFilename('archive.')).toBe('');
  });
});

describe('targetFormatsFor', () => {
  const resp: FormatsResponse = {
    engines: {
      ocr: { pairs: [['png', 'alto'], ['png', 'txt']] },
      pandoc: { pairs: [['docx', 'md'], ['docx', 'html'], ['md', 'docx']] },
      image: { pairs: [['jpg', 'png']] },
    },
  };

  it('filters targets to the pairs matching the given source, sorted', () => {
    expect(targetFormatsFor(resp, 'docx')).toEqual(['html', 'md']);
  });

  it('is case-insensitive on the source', () => {
    expect(targetFormatsFor(resp, 'PNG')).toEqual(['alto', 'txt']);
  });

  it('resolves aliases before matching (the half-broken case F3 calls out)', () => {
    expect(targetFormatsFor(resp, 'jpeg')).toEqual(['png']);
  });

  it('returns an empty list when the source has no registered pairs', () => {
    expect(targetFormatsFor(resp, 'zip')).toEqual([]);
  });

  it('returns an empty list when the source is empty', () => {
    expect(targetFormatsFor(resp, '')).toEqual([]);
  });
});

describe('targetFormatsOf', () => {
  it('returns the deduped, sorted global union of every pair target', () => {
    const resp: FormatsResponse = {
      engines: {
        ocr: { pairs: [['png', 'alto'], ['png', 'txt']] },
        pandoc: { pairs: [['docx', 'md'], ['docx', 'html'], ['md', 'docx']] },
        image: { pairs: [['jpg', 'png']] },
      },
    };
    expect(targetFormatsOf(resp)).toEqual(['alto', 'docx', 'html', 'md', 'png', 'txt']);
  });
});

describe('operationTable', () => {
  const first: FormatsResponse = {
    engines: {
      image: { pairs: [['png', 'webp']] },
      cad: { pairs: [['dxf', 'svg']] },
    },
    operations: {
      render: {
        zzz: { pairs: [['qqq', 'png']] },
        cad: { pairs: [['step', 'png'], ['dxf', 'png']] },
      },
      convert: { image: { pairs: [['png', 'webp']] } },
    },
  };

  const second: FormatsResponse = {
    engines: { probe: { pairs: [['pdf', 'pdf']] } },
    operations: {
      sanitize: { probe: { pairs: [['pdf', 'pdf']] } },
      frob: { yyy: { pairs: [['aaa', 'bbb'], ['aaa', 'ccc']] } },
    },
  };

  it('operationTable equals the response rows', () => {
    expect(operationTable(first)).toEqual([
      { operation: 'convert', engine: 'image', from: 'png', to: 'webp' },
      { operation: 'render', engine: 'cad', from: 'dxf', to: 'png' },
      { operation: 'render', engine: 'cad', from: 'step', to: 'png' },
      { operation: 'render', engine: 'zzz', from: 'qqq', to: 'png' },
    ]);
    expect(operationsOf(first)).toEqual(['convert', 'render']);
  });

  it('operationTable changes with the response', () => {
    const table = operationTable(second);
    expect(table).toEqual([
      { operation: 'frob', engine: 'yyy', from: 'aaa', to: 'bbb' },
      { operation: 'frob', engine: 'yyy', from: 'aaa', to: 'ccc' },
      { operation: 'sanitize', engine: 'probe', from: 'pdf', to: 'pdf' },
    ]);
    expect(table).not.toEqual(operationTable(first));
    expect(operationsOf(second)).toEqual(['frob', 'sanitize']);
    expect(operationTable({ engines: {}, operations: {} })).toEqual([]);
  });

  it('operationTable without the operations key is the engines under convert', () => {
    const older: FormatsResponse = {
      engines: {
        ocr: { pairs: [['png', 'txt']] },
        image: { pairs: [['jpg', 'png']] },
      },
    };
    expect(operationTable(older)).toEqual([
      { operation: 'convert', engine: 'image', from: 'jpg', to: 'png' },
      { operation: 'convert', engine: 'ocr', from: 'png', to: 'txt' },
    ]);
    expect(operationsOf(older)).toEqual(['convert']);
  });

  it('operationTable order is stable', () => {
    const shuffled: FormatsResponse = {
      engines: {},
      operations: {
        convert: { image: { pairs: [['png', 'webp']] } },
        render: {
          cad: { pairs: [['dxf', 'png'], ['step', 'png']] },
          zzz: { pairs: [['qqq', 'png']] },
        },
      },
    };
    const keys = operationTable(shuffled).map((r) => `${r.operation} ${r.engine} ${r.from} ${r.to}`);
    expect(keys).toEqual(['convert image png webp', 'render cad dxf png', 'render cad step png', 'render zzz qqq png']);
    expect(operationTable(shuffled)).toEqual(operationTable(first));
  });

  it('targetFormatsForOperation folds aliases', () => {
    const resp: FormatsResponse = {
      engines: {},
      operations: {
        convert: {
          cad: { pairs: [['dxf', 'svg'], ['dxf', 'pdf'], ['step', 'stl']] },
          image: { pairs: [['jpg', 'png']] },
        },
        render: { cad: { pairs: [['step', 'png'], ['dxf', 'png']] } },
      },
    };
    expect(targetFormatsForOperation(resp, 'render', 'stp')).toEqual(['png']);
    expect(targetFormatsForOperation(resp, 'convert', 'dxf')).toEqual(['pdf', 'svg']);
    expect(targetFormatsForOperation(resp, 'convert', 'STP')).toEqual(['stl']);
    expect(targetFormatsForOperation(resp, 'convert', 'jpeg')).toEqual(['png']);
    expect(targetFormatsForOperation(resp, 'sanitize', 'dxf')).toEqual([]);
    expect(targetFormatsForOperation(resp, 'render', '')).toEqual([]);
  });
});
