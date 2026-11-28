# ADR 0029: A routine's reply is a report, shown until seen

- Date: 2026-11-28
- Status: accepted; amends 0009 (what a run notifies)

## Context

Moss's **Daily check** runs at 08:30 and answers in its ongoing conversation: what is due today, what needs the user. Nothing pointed to that answer. A run that ended notified "The agent answered" only while a page was open in the background with desktop notifications on; a page that was closed, or in front on another session, showed nothing, so the user learned of the check only by opening Moss. Other routine replies ("no new mail") need nothing from the user, and a notification for each of them would be noise.

## Decision

1. **The turn's last reply is the run's report.** The workbench keeps the last message of the turn a routine's message started (Claude's `result`, Codex's last `agentMessage` of its own thread), up to 8 KiB.
2. **The agent says when it has nothing for the user.** Every routine message (not a background run's prompt) ends with a note from the workbench: the reply is shown to the user unless its last line is `[Nothing for you]`. A reply that ends so, or is empty, is kept but not shown. The transcript hides the note.
   - Showing is the default: an agent that forgets the mark costs one extra report, not a missed one.
3. **A report for the user waits until seen.** Its assistant's bell counts it, with the sessions that need the user and the runs waiting for approval, and so do the Assistants heading and the tab title. The assistant's card shows the reply itself under **Waiting for you**, with **Open** (its conversation) and **Dismiss**. Opening its conversation in a page in front counts as seen. A newer report of the same routine replaces an older unseen one.
4. **Notifications follow reports.** A run notifies when its report is for the user (with its first line) or when it failed, as before only while the page is in the background. A reply with nothing for the user no longer notifies.

## Consequences

- A daily check's answer is on Moss's row and card when the user next opens the workbench, without a notification for checks that found nothing.
- Every routine message carries one more sentence for the agent; the memory an assistant writes from its conversations sees it too.
- Whether something needs the user is the agent's judgment; the mark is plain text, so a reply quoting it on its last line stays quiet.
- Reports are routine runs, pruned with them.
