# ADR 0030: An icon for each assistant, with its agent as a badge

- Date: 2026-11-28
- Status: accepted; amends 0015 (the assistant's home card)

## Context

Every assistant was drawn as Moss's box camera, with the agent it runs on shown on the camera's screen. In the sidebar, a list of assistants looked alike: only the names told them apart. And the agent could be read only from that drawing, which an image of the user's own would not have.

## Decision

1. **Twelve built-in drawings, by what an assistant does:** `camera` (the box camera, now with a red lens and a blank screen; the default), `mail`, `review`, `video`, `calendar`, `bell`, `globe`, `chart`, `book`, `chat`, `doc` and `toolbox`. Each is pixel art at 16×16 (the sidebar; ink within the middle 14×14 like its other icons) and 32×32 (larger sizes, scaled by whole multiples), in the workbench's greys with one accent (`web/src/components/assistantIcons.ts`).
2. **The agent is a badge on any icon**, in its bottom-right corner, ringed in the background colour: the agent's own mark in a small tile from 48 pixels up (the card), a dot in the agent's colour below (the sidebar, the conversation's title). The camera no longer shows the agent on its screen: the badge alone does, for every icon alike.
3. **`icon` in `assistant.toml`** names a drawing, or a `.png` file in the assistant's folder: a plain file name, no path. Anything else makes the file invalid, like other settings. A file that is named but missing shows the camera. Without `icon`, the camera.
4. **Settings → Assistants** chooses a drawing (saved with the other settings), or uploads a picture:
   - the page cuts it to the square in its middle and scales it to 128×128 PNG;
   - the server takes only a 128×128 PNG of at most 512 KB, saves it as `icon.png` in the assistant's folder and sets `icon = "icon.png"`, committed together, so the icon travels with the assistant's repository;
   - it serves the file only as `image/png` with `nosniff`, at an address that changes with the file's time, so browsers may keep it;
   - removing the upload deletes the file and goes back to the camera.
5. **A new assistant** picks the drawing closest to its job (the create-assistant skill names them).

## Consequences

- Assistants are told apart at a glance in the sidebar, and their agent is read the same way on a drawing and on an uploaded image.
- An uploaded icon is an ordinary file of the assistants' repository: synced, reverted and shared with it.
- A new drawing needs both grids, the name in `builtinIcons` (Go) and in the skill's list.
