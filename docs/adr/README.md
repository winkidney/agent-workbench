# Architecture decision records

Decisions that shape how the workbench behaves, why, and what they cost. Each record keeps its number; a later decision that changes one says so in both.

| No. | Decision | Status |
| --- | --- | --- |
| [0001](0001-live-view-never-stalls.md) | The live view never stalls, and the page never stops connecting | accepted |
| [0002](0002-plan-continues-in-the-mode-it-left.md) | An approved plan continues in the mode the session left for it | accepted |
| [0003](0003-side-chat-hides-and-closes.md) | A side chat hides without ending, and closes only when asked | accepted; decision 4 amended by 0005 |
| [0004](0004-file-links-preview-or-locate.md) | A file link previews what the browser can show, and locates the rest in the file list | accepted; extended by 0007, amended by 0008 for media |
| [0005](0005-side-chat-steers-and-stops.md) | A side answer can be steered or stopped without closing the side chat | accepted |
| [0006](0006-terminal-tabs-live-until-closed.md) | A terminal tab lives until it is closed | accepted |
| [0007](0007-file-links-carry-a-line.md) | A file link may name lines, and a text file in the panel shows numbered lines | accepted |
| [0008](0008-media-shown-from-anywhere.md) | Images and videos a session names are shown wherever they are | accepted; amends 0004 for media |
| [0009](0009-routines-run-on-a-schedule.md) | Routines send messages and run commands on a schedule | accepted |
| [0010](0010-assistants-are-folders.md) | An assistant is a folder in a repository of assistants | accepted |
| [0011](0011-background-runs-commit-and-undo.md) | Background runs keep the wiki, read outside content, and commit what they change | accepted |
| [0012](0012-agents-schedule-their-own-messages.md) | Agents schedule their own messages and hand tasks to assistants through a local command | accepted |
| [0013](0013-assistants-set-up-on-first-use.md) | Assistants are set up on first use; each picks its agent; Moss makes new ones | accepted; amends 0010 |
| [0014](0014-moss-welcomes-one-template.md) | Moss welcomes the user; assistants come from one template and are backed up to GitHub | accepted |
| [0015](0015-an-assistant-explains-itself.md) | An assistant explains itself: metadata, a home card, its own settings | accepted; decision 5 amended by 0020 |
| [0016](0016-moss-ships-and-comes-back.md) | Moss ships with Agent Workbench and comes back from the user's repository | accepted |
| [0017](0017-assistants-choose-their-session-settings.md) | Assistants choose their sessions' model, effort, mode and permissions | accepted |
| [0018](0018-keep-the-computer-awake.md) | Two switches keep the computer awake while the workbench runs | accepted |
| [0019](0019-assistants-start-sessions-on-request.md) | An assistant starts a separate session when the user asks for one | accepted |
| [0020](0020-assistants-ask-through-the-client.md) | Assistants ask the user through the client's question tool, one step at a time | accepted; amends 0015 |
