# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Report what in the repository of assistants is not committed or not pushed.

Run by Moss's "Repository sync" routine. Prints nothing when every folder is
committed and, if the branch has a remote, nothing waits to be pushed or
pulled, so the routine does not wake Moss. Changes nothing itself: Moss asks
each assistant to commit its own folder, and pushes.

Output, one line each:
  uncommitted <folder> <n> files   (an assistant's folder, "shared" or ".")
  ahead <n>                        (commits not on the remote yet)
  behind <n>                       (remote commits not here yet)
  fetch-failed <reason>            (remote set but unreachable)
No remote for the current branch: only the uncommitted lines.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def git(*args: str, root: Path = ROOT) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", "-C", str(root), *args], capture_output=True, text=True
    )


def owner(path: str, root: Path) -> str:
    """The folder a changed path belongs to: an assistant, shared, or '.'."""
    top = path.split("/", 1)[0]
    if "/" in path and (root / top / "assistant.toml").exists():
        return top
    if top == "shared":
        return "shared"
    return "."


def uncommitted(root: Path = ROOT) -> dict[str, int]:
    out = git("status", "--porcelain", "-z", "--untracked-files=all", root=root).stdout
    counts: dict[str, int] = {}
    entries = out.split("\0")
    i = 0
    while i < len(entries):
        entry = entries[i]
        i += 1
        if len(entry) < 4:
            continue
        status, path = entry[:2], entry[3:]
        if "R" in status or "C" in status:
            i += 1  # the original path of a rename or copy follows
        who = owner(path, root)
        counts[who] = counts.get(who, 0) + 1
    return counts


def remote_state(root: Path = ROOT) -> list[str]:
    upstream = git("rev-parse", "--abbrev-ref", "@{upstream}", root=root)
    if upstream.returncode != 0:
        return []  # no remote for this branch: nothing to sync
    fetch = git("fetch", "--quiet", root=root)
    if fetch.returncode != 0:
        reason = (fetch.stderr.strip().splitlines() or ["unknown"])[-1]
        return [f"fetch-failed {reason}"]
    counts = git("rev-list", "--left-right", "--count", "HEAD...@{upstream}", root=root)
    ahead, behind = (int(n) for n in counts.stdout.split())
    lines = []
    if ahead:
        lines.append(f"ahead {ahead}")
    if behind:
        lines.append(f"behind {behind}")
    return lines


def report(root: Path = ROOT) -> list[str]:
    lines = [
        f"uncommitted {folder} {n} files"
        for folder, n in sorted(uncommitted(root).items())
    ]
    return lines + remote_state(root)


def main() -> int:
    for line in report():
        print(line)
    return 0


if __name__ == "__main__":
    sys.exit(main())
