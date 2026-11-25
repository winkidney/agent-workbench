# ADR 0024: The side chat shares the right panel with the Outputs

- Date: 2026-11-25
- Status: accepted; amends [0003](0003-side-chat-hides-and-closes.md) (where the panel shows, and what Esc does)

## Context

The side chat was a drawer laid over the conversation, 560px wide at most and fixed, while the sidebar and the Outputs were columns beside it that could be dragged and were remembered. Opened with the Outputs, it covered them and part of the conversation. Its open state was forgotten on reload. On a phone it took the whole screen, header included, where the Outputs were a drawer under the header. The header's Outputs button was called "Toggle preview" and did not say whether the panel showed.

Esc hid the side chat wherever focus was. The panel, always mounted while a session was open, was a `role="dialog"`, and the conversation's Esc handler steps aside while a dialog is on the page: so Esc in the conversation no longer interrupted the turn.

## Decision

1. **One right panel, two views.** The right column shows a session's side chat or the Outputs. The header's two buttons work like tabs: the one showing is pressed, and clicking it hides the column; clicking the other switches to it. Hiding the side chat brings the Outputs back if they were open. Opening a file or a link shows the Outputs; the side chat keeps going behind, and its button carries the dot of ADR 0003.
2. **The Outputs stay mounted** while the side chat shows in their place, so preview tabs and an unsaved edit stay.
3. **The width is dragged and remembered**, as for the Outputs: the side chat has its own width (480px until dragged; a double-click returns to it), and which sessions show their side chat is remembered per browser.
4. **Esc follows focus.** In the side chat, Esc stops a running answer from its box, and otherwise hides the panel. In the conversation, Esc interrupts the turn. The panel is a complementary region, not a dialog.
5. **One look.** Both panels have the same 48px heading (icon, name, ghost buttons, a hide button) and background. On a narrow screen both are the same drawer under the header, and only one drawer (sessions, Outputs or side chat) is open at a time.

## Consequences

- The side chat and the Outputs cannot be seen side by side; a file opened from a side answer switches the column to the Outputs.
- With the sidebar open on a 1280px screen, the side chat grows to about 570px at most: the conversation keeps its minimum width.
