# ADR 0026: A preview's links to Markdown files beside it open in place

- Date: 2026-11-27
- Status: accepted; amends 0023 (decision 2, links that do nothing)

## Context

ADR 0023 shows a routine's preview on the assistant's card, rendered as Markdown with links that do nothing, because the preview carries outside content (a mail's sender and subject). The mail assistant's preview lists each mail with an **open** link to its file next to the preview (`raw/mail/<date>/<id>.md`). The link did nothing, so the user could see who wrote and what about, but not what a mail says, before deciding to approve it. Reading the mail first is the point of the preview.

A mail's file keeps its headers and body in one code block, so rendering it as Markdown shows it as plain text: no link, image or HTML from the mail becomes live.

## Decision

1. **The workbench reads a file the preview links to.** `GET /api/routines/{id}/preview?file=<path>` returns `{text}` for a path relative to the preview file's folder. Only a `.md` file that is still inside that folder after following links, a regular file up to 1 MB; anything else is refused, and a missing file is 404. The folder is the preview's own, not the whole assistant's: the mail preview reaches `raw/mail/`, not `config.toml`, `tools/` or `state/`.
2. **The card shows it in place.** A relative `.md` link in the preview replaces the list with that file, rendered as Markdown with links that do nothing at all, and a **Back to the list** button that restores the list where it was scrolled. Other links in the preview still do nothing.
3. Approve still waits for the preview itself (ADR 0023); opening a file is not required.

## Consequences

- The user can read a mail's body on the card before approving it, without an agent.
- An assistant whose preview sits at the top of its folder lets the card open any `.md` file in that folder (its `AGENTS.md`, its state). Put the preview in the folder its files live in, as the mail assistant does.
- The check is on the server: the page only sends relative `.md` paths, but the server refuses any other path, absolute or `..`, and links out of the folder.
