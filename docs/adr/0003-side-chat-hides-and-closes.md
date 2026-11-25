# ADR 0003: A side chat hides without ending, and closes only when asked

- Date: 2026-11-16
- Status: accepted; decision 4 amended by [ADR 0005](0005-side-chat-steers-and-stops.md) (a running answer can be steered or stopped); where the panel shows and what Esc does amended by [ADR 0024](0024-side-chat-shares-the-right-panel.md)

## Context

A side chat holds resources in its parent's engine: a Claude side request or a forked Codex thread, and the parent engine stays awake while it lives. Closing it releases them and turns it into a read-only record.

The panel offered only that: its close button and Esc both asked to close and release the side chat. There was no way to put it out of the way and come back to it; cancelling the question kept the panel over the conversation. A confirmed close also removed the panel, so the record just closed disappeared, and reopening showed an empty panel. Whether a side chat still lived, or was working, could not be seen once the panel was gone.

While a question ran, the panel showed a small "Running" label and a disabled box. A Claude side question answers in one piece, and a sleeping parent engine first has to start, so for tens of seconds nothing said what was happening. The side stream also polled once a second, so even the sent message appeared late.

## Decision

1. **Hiding and closing are separate.** Hiding (Esc, the panel's hide button, or the header's side chat button) only hides the panel; the side chat, its stream and an unsent draft stay. Closing (the panel's close button, confirmed) releases the side chat and keeps its read-only record in view.
2. **The panel belongs to its session.** Each session remembers whether its panel is shown. The panel stays mounted while its session is open, shown or hidden, and is released when another session is opened.
3. **The header says when a side chat lives.** While the panel is hidden, the side chat button carries a dot: green for a live side chat, pulsing amber while it works on an answer.
4. **A running question says what it waits on, and for how long.** A side chat records when its question was sent (`since`) and what it waits on (`phase`: `waking` while the parent engine starts, then `answering`). The panel shows the conversation's status line with that and the elapsed time. A sent message shows at once, marked as sending, until the server's copy arrives. The box stays editable for the next question; sending waits for the answer.
5. **The side stream is pushed.** It wakes on every change, as the conversation stream does, sends its status only when it changed, and keeps the connection with a comment every 15 seconds.

## Consequences

- A hidden side chat keeps its parent engine awake until it is closed, as before; the header dot makes that visible.
- Opening a session reads its side chat list once, and a live side chat's stream stays open while that session is open, shown or not.
- Esc no longer asks to close a side chat; closing takes the close button and a confirmation.
