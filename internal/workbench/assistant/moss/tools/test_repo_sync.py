# /// script
# requires-python = ">=3.11"
# dependencies = ["pytest"]
# ///
"""Tests for repo_sync.py on throwaway repositories in a temp folder."""

import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import repo_sync  # noqa: E402


def run(root: Path, *args: str) -> None:
    subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True)


def make_repo(path: Path) -> Path:
    path.mkdir()
    run(path, "init", "-q", "-b", "master")
    run(path, "config", "user.email", "test@example.invalid")
    run(path, "config", "user.name", "test")
    (path / "mail").mkdir()
    (path / "mail" / "assistant.toml").write_text('name = "mail"\n')
    (path / "AGENTS.md").write_text("rules\n")
    run(path, "add", "-A")
    run(path, "commit", "-q", "-m", "init")
    return path


def test_clean_without_remote_prints_nothing(tmp_path):
    repo = make_repo(tmp_path / "repo")
    assert repo_sync.report(repo) == []


def test_uncommitted_grouped_by_owner(tmp_path):
    repo = make_repo(tmp_path / "repo")
    (repo / "mail" / "log.md").write_text("x\n")
    (repo / "mail" / "assistant.toml").write_text('name = "mail2"\n')
    (repo / "shared").mkdir()
    (repo / "shared" / "note.md").write_text("x\n")
    (repo / "AGENTS.md").write_text("changed\n")
    (repo / "notes").mkdir()
    (repo / "notes" / "a.md").write_text("x\n")  # no assistant.toml: root's
    assert repo_sync.report(repo) == [
        "uncommitted . 2 files",
        "uncommitted mail 2 files",
        "uncommitted shared 1 files",
    ]


def test_ahead_and_behind_with_remote(tmp_path):
    remote = tmp_path / "remote.git"
    subprocess.run(["git", "init", "-q", "--bare", "-b", "master", str(remote)], check=True)
    repo = make_repo(tmp_path / "repo")
    run(repo, "remote", "add", "origin", str(remote))
    run(repo, "push", "-q", "-u", "origin", "master")
    assert repo_sync.report(repo) == []

    other = tmp_path / "other"
    subprocess.run(["git", "clone", "-q", str(remote), str(other)], check=True)
    run(other, "config", "user.email", "test@example.invalid")
    run(other, "config", "user.name", "test")
    run(other, "commit", "-q", "--allow-empty", "-m", "remote change")
    run(other, "push", "-q")

    run(repo, "commit", "-q", "--allow-empty", "-m", "local change")
    assert repo_sync.report(repo) == ["ahead 1", "behind 1"]


def test_unreachable_remote(tmp_path):
    remote = tmp_path / "remote.git"
    subprocess.run(["git", "init", "-q", "--bare", "-b", "master", str(remote)], check=True)
    repo = make_repo(tmp_path / "repo")
    run(repo, "remote", "add", "origin", str(remote))
    run(repo, "push", "-q", "-u", "origin", "master")
    run(repo, "remote", "set-url", "origin", str(tmp_path / "missing.git"))
    [line] = repo_sync.report(repo)
    assert line.startswith("fetch-failed ")
