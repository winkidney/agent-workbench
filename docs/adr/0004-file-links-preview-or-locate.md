# ADR 0004: A file link previews what the browser can show, and locates the rest in the file list

- Date: 2026-11-17
- Status: accepted; extended by 0007; amended by 0008 (media from anywhere)

## Context

Paths in replies are links. Images and videos opened in the viewer, and HTML, PDF and SVG in the preview panel; every other path, Markdown and source code included, opened the desktop file manager (or downloaded, away from the workbench computer). The file a session most often writes for you to read, a Markdown report or plan, thus left the workbench for another program, and a stray click on a log or JSON file opened a file manager window. The small folder button next to a path chip did the same.

The preview panel already had a file list (the **Files** tab), but nothing led to it from a link, and it had no way to reach the file manager.

## Decision

1. **What the browser can show opens in the preview panel.** Besides images, video, HTML, PDF and SVG: Markdown, drawn with the conversation's renderer (no HTML run; links and images relative to the file), and text (JSON, logs, source code, configuration), shown highlighted. A link is classified by its name; the panel then asks the server whether the file really is UTF-8 text up to 1 MB (the editor's limit) before drawing it so, and shows anything else as the browser would.
2. **Anything else is located in the file list.** Archives, audio, binaries and folders open the **Files** tab at the item's folder, with the item marked and scrolled to. The server resolves the path as it does for previews (`GET /api/resolve`), and lists the item even where the list hides such entries (`files?show=`). The button beside a path chip locates too.
3. **The file manager is reached from the file list.** Each entry has a menu (right click, long press on touch screens, the context-menu key or Shift+F10) with **Open in system file manager**, or **Download** away from the workbench computer. Links no longer open it directly. The media viewer keeps its own button.

## Consequences

- Reading what an agent wrote stays in the workbench, on every device, since text is read through the page's own API rather than a preview origin.
- Opening the file manager takes one more step (locate, then the menu), on purpose: it is a window outside the page.
- The text shown lives only in its tab's view, released with it; files over 1 MB are not drawn as Markdown or highlighted.
