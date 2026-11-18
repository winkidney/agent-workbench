# ADR 0007: A file link may name lines, and a text file in the panel shows numbered lines

- Date: 2026-11-18
- Status: accepted; extends 0004

## Context

Both CLIs cite code as a link to a path followed by a line, as editors do: `[x](/project/src/fx.gd:127)`. The page took `:127` for part of the file name, so the link was neither previewed nor found (`/api/resolve` and `/api/text` answered 404), and clicking it did nothing. The preview panel drew a text file as one highlighted block without line numbers, so a line could not be pointed at either.

## Decision

1. **A link's trailing line reference is read off the path.** `:12`, `:12:5`, `:12-20`, `:12:5-20:3`, and GitHub's `#L12`, `#L12C5`, `#L12-L20` name a line or range (columns are ignored). The path without it is what is previewed or located; only links that open in the preview panel use the lines. Inline code that names a path is unchanged.
2. **A text file in the panel shows numbered lines.** Each line of the highlighted block is its own element with its number drawn beside it (not part of the text, so copying is unchanged); a token spanning lines keeps its color on each.
3. **The lines a link named are marked and scrolled to,** centred, again on every opening of the link, even when the tab is already open. Opening the file without lines clears the mark. The lines belong to the opening, not the tab the server keeps.
4. **A Markdown file opened at a line shows its source,** numbered like any text file, since the drawn document has no lines to point at. Opened without one, it is drawn as before.

## Consequences

- Links agents write to cite code open where they point, on every device.
- A long text file's block is wider than before by its line-number column; long lines still scroll sideways, never wrap.
- A file whose name really ends in `:<digits>` cannot be linked by that name; such names are rare and the file stays reachable from the file list.
