# ADR 0008: Images and videos a session names are shown wherever they are

- Date: 2026-11-19
- Status: accepted; amends 0004 for media

## Context

The media route served only files inside the session's launch directory, and the page did not even ask for anything else. Agents often put what they show the user elsewhere: screenshots and side-by-side comparisons under `/tmp`, images Codex generated under `~/.codex/generated_images/<thread>/`. Such images, in a ViewImage or GenerateImage row, a Markdown image or a link, showed as a bare path or nothing, with no word of why.

The agents already read and write anywhere the user can, and every `/api/` route is behind the workbench's sign-in.

## Decision

1. **`/api/media` serves images and videos from any path a session wrote**: absolute (or `file://`), or relative to where the agent was, else to the launch directory; symlinks are followed. Inside the launch directory, paths resolve as before.
2. **What it serves is unchanged otherwise**: the file is sniffed as a raster image or a video (no SVG, no HTML), images up to 32 MB, sent under a sandboxing CSP.
3. **Previews, text, editing, downloads and the file list stay within the launch directory.**
4. **The page asks for media outside the working directory by its absolute path** (thumbnails, Markdown images, links that open the viewer). Such items carry no path, so the viewer offers no Show in Files or Download for them.
5. **An image that does not load says why**: the page asks the server for its status and shows "not found" or "not an image" beside it, and the viewer says the same.

## Consequences

- Images and videos the agents point to show up wherever the agents saved them.
- Any signed-in browser, including one coming through a tunnel, can view any image or video the workbench's user can read, by naming its path. Sign-in is what protects them; the user chose this over listing which images a session viewed.
