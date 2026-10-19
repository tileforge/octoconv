import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// Mirrors internal/mcpserver/no_text_matching_test.go's Go-side scan so the
// no-message-text-branching proof survives even if the Go test's file paths
// drift (ERRC-03, PITFALLS.md Pitfall 12). Keep the two pattern lists in
// sync -- see the comment on messageBranchPattern below and its Go twin.

// messageBranchPattern is deliberately anchored to `.message.<call>(` and
// equality comparisons against `error.message` only -- NOT a blanket ban on
// `.includes`/`.startsWith`/etc, which this codebase uses legitimately
// elsewhere for unrelated string handling. If tempted to broaden this,
// don't: narrow it back to the message-branching expressions below, or
// unrelated code starts failing this test for no reason.
const messageBranchPattern =
  /\.message\.(includes|startsWith|indexOf|match)\(|error\.message\s*(===|==|!==|!=)|(===|==|!==|!=)\s*error\.message/;

const srcDir = dirname(fileURLToPath(import.meta.url));

function scanFileForMessageBranch(relativeFile: string): string[] {
  const path = join(srcDir, relativeFile);
  const text = readFileSync(path, 'utf-8');
  const hits: string[] = [];

  text.split('\n').forEach((line, index) => {
    const trimmed = line.trim();
    if (trimmed.startsWith('//') || trimmed.startsWith('*')) {
      return;
    }
    if (messageBranchPattern.test(line)) {
      hits.push(`${relativeFile}:${index + 1}: ${trimmed}`);
    }
  });

  return hits;
}

describe('no consumer branches on error message text (ERRC-03)', () => {
  it('API and workspace error consumers contain no message-text branch', () => {
    const violations = [
      ...scanFileForMessageBranch('api.ts'),
      ...scanFileForMessageBranch('App.tsx'),
      ...scanFileForMessageBranch('errors.ts'),
      ...scanFileForMessageBranch('Workspace.tsx'),
    ];

    expect(violations, `found message-text branch(es):\n${violations.join('\n')}`).toEqual([]);
  });
});
