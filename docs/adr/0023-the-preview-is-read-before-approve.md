# ADR 0023: The preview shows on the assistant's page, and Approve waits for it

- Date: 2026-11-24
- Status: accepted; amends 0021 (decision 1, the home card's Preview); decision 2 amended by [0026](0026-preview-links-open-files-beside-it.md) (links to Markdown files beside the preview open in place)

## Context

ADR 0021 put **Preview** on the assistant's home card: it opened the routine's preview file in the preview panel. The panel belongs to a session. The page where the card usually shows, an assistant's new-request page, has none, so the card showed only **Approve** and **Dismiss**. With more than one waiting run, the card did not say where the preview was either. The user could approve mail without having seen who sent it, which the approval step exists to prevent.

## Decision

1. **The workbench reads the preview itself.** `GET /api/routines/{id}/preview` returns `{text}`, the file the routine's `preview` names. It reads only a regular file inside the assistant's folder after following links, up to 1 MB, and answers 404 while the file is not there yet.
2. **The card shows it in place.** **Preview** opens the file below the card's buttons, on any page, rendered as Markdown with no HTML and with links that do nothing. The card names the file's path.
3. **Approve waits for the preview.** When the routine names a preview, **Approve** stays off until the user has opened the preview for the runs now waiting; a newer waiting run turns it off again. **Dismiss** never waits: it sends nothing.

## Consequences

- Mail reaches an agent only after its senders and subjects were on the user's screen.
- The check is on the page only: `POST …/approve` still accepts a run whose preview nobody opened (an older page, a script).
