from __future__ import annotations

import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path


class GitError(RuntimeError):
    pass


def _git(repo: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    proc = subprocess.run(
        ["git", "-C", str(repo), *args],
        text=True,
        capture_output=True,
    )
    if check and proc.returncode != 0:
        raise GitError(proc.stderr.strip() or proc.stdout.strip() or "git command failed")
    return proc


@dataclass(slots=True)
class Worktree:
    path: Path
    branch: str
    base_commit: str
    detached: bool = False


class GitRepo:
    def __init__(self, repo: Path, worktree_root: Path):
        self.repo = repo.resolve()
        self.worktree_root = worktree_root.resolve()

    def validate(self, allow_dirty: bool = False) -> None:
        if not self.repo.exists():
            raise GitError(f"repository does not exist: {self.repo}")
        inside = _git(self.repo, "rev-parse", "--is-inside-work-tree").stdout.strip()
        if inside != "true":
            raise GitError(f"not a git worktree: {self.repo}")
        if not allow_dirty and _git(self.repo, "status", "--porcelain").stdout.strip():
            raise GitError(
                "target repository has uncommitted changes; commit/stash them or set allow_dirty_repo=true"
            )

    def head(self) -> str:
        return _git(self.repo, "rev-parse", "HEAD").stdout.strip()

    def add_worktree(
        self,
        run_id: str,
        label: str,
        base_commit: str,
        branch: str | None,
    ) -> Worktree:
        path = self.worktree_root / run_id / label
        if path.exists():
            shutil.rmtree(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        if branch:
            _git(self.repo, "worktree", "add", "-b", branch, str(path), base_commit)
            return Worktree(path=path, branch=branch, base_commit=base_commit)
        _git(self.repo, "worktree", "add", "--detach", str(path), base_commit)
        return Worktree(path=path, branch="(detached)", base_commit=base_commit, detached=True)

    def commit_all(self, worktree: Worktree, message: str) -> str:
        status = _git(worktree.path, "status", "--porcelain").stdout.strip()
        if not status:
            return _git(worktree.path, "rev-parse", "HEAD").stdout.strip()
        _git(worktree.path, "add", "-A")
        proc = subprocess.run(
            [
                "git",
                "-C",
                str(worktree.path),
                "-c",
                "user.name=AVO Harness",
                "-c",
                "user.email=avo-harness@localhost",
                "commit",
                "-m",
                message,
            ],
            text=True,
            capture_output=True,
        )
        if proc.returncode != 0:
            raise GitError(proc.stderr.strip() or proc.stdout.strip())
        return _git(worktree.path, "rev-parse", "HEAD").stdout.strip()

    def remove_worktree(self, worktree: Worktree) -> None:
        _git(self.repo, "worktree", "remove", "--force", str(worktree.path), check=False)
        if worktree.path.exists():
            shutil.rmtree(worktree.path, ignore_errors=True)

    def point_branch(self, branch: str, commit_sha: str) -> None:
        _git(self.repo, "branch", "-f", branch, commit_sha)
