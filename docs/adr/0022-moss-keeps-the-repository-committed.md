# ADR 0022: Moss has the assistants commit their changes each evening

- Date: 2026-11-23
- Status: accepted; amends 0016 (Moss's definition also carries its tools)

## Context

The scheduler pushes the commits `origin` lacks (ADR 0014), and background runs commit what they change (ADR 0011). What a conversation changes is committed only if that conversation does it. An assistant that edits its code in a conversation and stops before committing leaves the change on one computer, where no backup reaches it. The workbench also never pulls, so a repository that another computer pushed to stays behind, and the automatic push then fails.

## Decision

1. **A routine, not the workbench.** Moss's `assistant.toml` gets a routine "Repository sync", daily at 22:00. It runs `uv run tools/repo_sync.py` in Moss's folder. The script prints one line for each folder with uncommitted files (an assistant's folder, `shared`, or `.` for the rest). With an upstream branch, it fetches, and also prints how many commits are ahead and behind, or that the fetch failed. When there is nothing to do it prints nothing, so Moss is not woken.
2. **Each assistant commits its own folder.** Moss sends an assistant with uncommitted files a message to commit them. Moss commits only its own folder and the wiki. It reports other top-level files to the user and leaves them uncommitted. The scheduler pushes the new commits as before.
3. **Moss catches up with the remote.** When the branch is behind, or commits are still unpushed by the evening, Moss runs `git pull --rebase` and `git push`. It stops on a conflict and tells the user. A repository without a remote is left alone.
4. **The script ships with Moss.** `main/tools/repo_sync.py` and its test join Moss's definition (`moss/tools/`). They update like the other definition files, and `moss export` carries them back (0016). The routine comes from the template. A repository set up before this version has no routine until the user or Moss adds it.

## Consequences

- Uncommitted work is noticed within a day, and the assistant that owns it commits it. No assistant commits another assistant's files.
- An assistant whose conversations ask before each command (a mail assistant, ADR 0021) asks the user to approve the commit.
- A pull can bring changes into the repository. Moss does it in the evening in a conversation, never in a background run, and stops on any conflict.
