# ADR 0005: A side answer can be steered or stopped without closing the side chat

- Date: 2026-11-18
- Status: accepted
- Amends: [ADR 0003](0003-side-chat-hides-and-closes.md), decision 4 ("sending waits for the answer")

## Context

ADR 0003 kept the side chat's box editable while an answer ran, but sending waited for the answer to end. A side answer can run for minutes: Codex runs tools, and may start sub-agents. Once a question was on its way there was no way to correct it, add to it, or give it up, short of closing the whole side chat — which releases it and leaves only a read-only record.

The main conversation already does both: a message sent while Codex works is steered into the running turn (`turn/steer`), and the stop button interrupts the turn.

## Decision

1. **A message sent while Codex answers steers that answer.** The side chat sends it with `turn/steer` to the side's own thread, naming the running turn, as the main conversation does. It shows in the record marked as steered. While the parent engine is still waking, or before the turn has started, the message is refused and stays in the box.
2. **Claude's side answer is not steered.** Claude answers a side question in one request; a message sent meanwhile is refused with a hint to stop the answer first.
3. **Stop ends the answer, not the side chat.** While an answer runs, the box shows a stop button next to send. It interrupts the Codex turn (`turn/interrupt`), cancels the Claude request (`control_cancel_request`), or, before either has started, stops waking or forking. The side chat goes back to idle with a "Stopped" line and no error; the question comes back to the box (unless something else is written there) to be edited and asked again.
4. **A question card's interrupt stops the answer.** In a side chat, interrupting from a question or approval card stops the answer instead of asking to close the side chat.

## Consequences

- Closing remains the only way to release a side chat; stopping keeps it, and its parent engine awake, as hiding does.
- A stop that arrives before Codex names its turn is remembered: the side either never starts the turn, or interrupts it as soon as it starts.
- A Claude request that does not answer its cancellation within 5 seconds is no longer waited on.
