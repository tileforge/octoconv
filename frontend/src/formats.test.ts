import { describe, expect, it } from 'vitest';
import { normalizeFormat, sourceFormatOfFilename, targetFormatsFor, targetFormatsOf } from './formats';
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
