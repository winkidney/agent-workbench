#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Save and check the credentials assistants use, outside any conversation.

The user runs this in a terminal (the panel's Terminal tab works):
  uv run ../shared/tools/secret.py set NAME            asks with hidden input
  uv run ../shared/tools/secret.py set NAME --from F   copies a file (an OAuth client JSON)
  uv run ../shared/tools/secret.py check NAME          says whether it is there; never its value
  uv run ../shared/tools/secret.py open URL            opens a sign-in page in the browser
Secrets live in ~/.config/assistant/secrets/NAME, mode 600. Nothing here
prints a value, so an assistant can run `check` and see only the answer.
"""

from __future__ import annotations

import getpass
import os
import re
import sys
import webbrowser
from pathlib import Path

SECRETS = Path.home() / ".config" / "assistant" / "secrets"


def path_of(name: str, folder: Path | None = None) -> Path:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", name):
        raise SystemExit(f"{name!r}: letters, digits, . _ - only")
    return (folder or SECRETS) / name


def save(name: str, value: str, folder: Path | None = None) -> Path:
    if not value.strip():
        raise SystemExit("nothing to save")
    p = path_of(name, folder)
    p.parent.mkdir(parents=True, exist_ok=True)
    if os.name != "nt":
        os.chmod(p.parent, 0o700)
    fd = os.open(p, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write(value if value.endswith("\n") else value + "\n")
    if os.name != "nt":
        os.chmod(p, 0o600)
    return p


def check(name: str, folder: Path | None = None) -> tuple[bool, str]:
    p = path_of(name, folder)
    if not p.exists():
        return False, f"{name}: missing (save it with: uv run ../shared/tools/secret.py set {name})"
    if os.name != "nt" and p.stat().st_mode & 0o077:
        return False, f"{name}: readable by others; fix with: chmod 600 {p}"
    if not p.read_text(encoding="utf-8").strip():
        return False, f"{name}: empty"
    return True, f"{name}: saved"


def main(argv: list[str]) -> int:
    if len(argv) >= 2 and argv[0] == "set":
        name = argv[1]
        if len(argv) == 4 and argv[2] == "--from":
            value = Path(os.path.expanduser(argv[3])).read_text(encoding="utf-8")
        else:
            value = getpass.getpass(f"{name} (hidden, Enter to save): ")
        save(name, value)
        print(f"Saved {name}. Go back to the conversation and say it is done.")
        return 0
    if len(argv) == 2 and argv[0] == "check":
        ok, line = check(argv[1])
        print(line)
        return 0 if ok else 1
    if len(argv) == 2 and argv[0] == "open":
        if not re.match(r"https://", argv[1]):
            raise SystemExit("only https:// pages")
        print("Opening " + argv[1])
        webbrowser.open(argv[1])
        return 0
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
