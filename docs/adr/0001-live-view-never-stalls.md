# ADR 0001: The live view never stalls, and the page never stops connecting

- Date: 2026-11-13
- Status: accepted

## Context

A conversation stopped updating while the page said it was connected. The agent's reply arrived, then the user's next message and the reply to it, and the page showed none of them, not even the message the user had just sent. The session list kept updating, so a background task the agent had started was visible. Reloading the page showed everything.

The workbench had done its part. Every one of those records was in its event log under the right session, and the event stream was delivering them: a fresh page showed them live.

The page had stopped applying them. Live events are batched into one update per animation frame: an incoming event is queued, and a frame is requested only when none is pending. Switching sessions, or reloading the conversation's history, cancelled the pending frame but left its id behind. From then on every event was queued behind a frame that would never run, until the page was reloaded. A frame is pending at a switch whenever another session is streaming at that moment. In a background tab it is pending all the time, because browsers do not run animation frames for hidden pages.

The user's own message vanished for a related reason. The page showed a sent message until the send request returned, then left it to the server's echo, and the echo was stuck in the same queue.

Separately, the stream itself could stop for good. A browser closes an event stream permanently when a reconnect gets an HTTP error, for example from a proxy while the workbench restarts. A stream can also stall without closing, when a connection dies quietly (sleep, a network change). The page showed "Disconnected" only in the sidebar's footer, which a phone does not show, and waited for someone to press Retry.

## Decision

1. **Live events are applied by a batch that can be reset completely.** Resetting it cancels the pending frame, empties the queue and forgets the frame, so the next event always schedules a new one. A page that becomes visible again applies whatever waited.
2. **A sent message stays on screen until its echo arrives.** It goes away early only if the send fails (its text returns to the composer) or the user switches sessions.
3. **The page never stops connecting.** When the stream is closed or fails, the page connects again by itself, waiting a little longer each time (1, 2, 5, 10, 20, then every 30 seconds) and starting over once connected. There is no state where it waits for the user; Retry only connects at once.
4. **A stalled stream is noticed and replaced.** The workbench sends a small `ping` event after 15 quiet seconds (a comment line, as before, is invisible to the page). If nothing at all arrives for 45 seconds, the page opens a new stream. It also checks as soon as it becomes visible, comes back online or is restored from the back-forward cache, and reconnects at once if the stream has been quiet for 20 seconds. A new stream starts after the last event the page received, so the workbench sends what was missed from its log.
5. **An interruption is visible everywhere.** When live updates have been down for more than 5 seconds, a line at the top of the conversation says so, with Retry, on phones too.

## Consequences

- A quiet stream carries a 30-byte `ping` every 15 seconds, about 7 KB an hour.
- After a reconnect, some events may arrive twice. The conversation is keyed by record, so applying one again changes nothing.
- A workbench that is down stays retried every 30 seconds for as long as the page is open.
- While a tab is hidden, live events wait in memory and are applied together when it is shown, as before.

## Related

- `web/src/transcript/frameBatch.ts`, `web/src/transcript/useTranscript.ts`
- `web/src/App.tsx` (the stream, the sent message), `web/src/lib/connection.ts`
- `internal/workbench/server.go` (`events`)
- [Usage: access from other devices](../usage.md#access-from-other-devices)
