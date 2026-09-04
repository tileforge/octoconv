# tectonic test fixtures

## `hostile_write18.tex`

Asks TeX to run `touch` through `\write18`, TeX's shell-escape primitive.

`OCTOCONV_SENTINEL_PATH` is a placeholder the test replaces with an absolute
path in its own temp directory. It must stay absolute: with a relative path the
fixture looked harmless and was not — tectonic runs shell commands in its own
working directory, so the file was created somewhere the test never looked, and
the "no sentinel" assertion passed for the wrong reason.

### Measured, 2026-09-04, tectonic 0.17.0, against the reduced bundle

| configuration | exit | sentinel |
|---|---|---|
| `--untrusted` (the production argv) | 0 | absent |
| `--untrusted` **and** an explicit `-Z shell-escape` | 0 | absent |
| `-Z shell-escape`, no `--untrusted` | 0 | **present** |

The middle row is the requirement: `--untrusted` makes shell-escape
un-enableable *regardless of other flags*, which is a structural guarantee
rather than a default a later argument can undo. The last row is what makes the
middle row worth anything — without it, "no sentinel" would be equally
consistent with a fixture that never executes.

## What TEX-04 names that tectonic does not implement

TEX-04 names two vectors: `\write18{...}` and `\input{|"command"}`. The second
one **cannot work in tectonic at all**, and the engine says so:

```
warning: piped inputs from external commands are not implemented in Tectonic
error: ! LaTeX Error: File `|touch ....tex' not found.
```

There is no fixture for it, because there is nothing to prove: the vector is
absent from the implementation rather than blocked by our configuration. Stated
here rather than quietly satisfied by a test that would have passed for a reason
unrelated to anything this project does.

It also cost the first fixture its meaning: both vectors were in one document,
and the `\input` line raised a hard error that aborted the run before
`\write18`'s effect could be observed. One vector per document.
