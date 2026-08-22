from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from avo_harness.config import AVOConfig, EvaluatorConfig, SupervisorConfig, WorkerConfig
from avo_harness.orchestrator import Orchestrator


def git(repo: Path, *args: str) -> str:
    proc = subprocess.run(["git", "-C", str(repo), *args], text=True, capture_output=True, check=True)
    return proc.stdout.strip()


def test_end_to_end_candidate_lineage(tmp_path: Path) -> None:
    repo = tmp_path / "repo"
    repo.mkdir()
    git(repo, "init")
    (repo / "README.md").write_text("start\n", encoding="utf-8")
    git(repo, "add", ".")
    subprocess.run(
        ["git", "-C", str(repo), "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init"],
        check=True,
        capture_output=True,
        text=True,
    )

    worker_script = (
        "from pathlib import Path; "
        "Path('answer.txt').write_text('good\\n', encoding='utf-8'); "
        "print('implemented answer')"
    )
    evaluator_script = (
        "import json, pathlib, sys; "
        "p=pathlib.Path('answer.txt'); ok=p.exists() and p.read_text().strip()=='good'; "
        "print(json.dumps({'score': 1.0 if ok else 0.0, 'summary': 'ok' if ok else 'missing'})); "
        "sys.exit(0 if ok else 1)"
    )
    config = AVOConfig(
        repo=str(repo),
        state_dir=str(tmp_path / "state"),
        max_iterations=2,
        worker=WorkerConfig(backend="command", command=[sys.executable, "-c", worker_script]),
        supervisor=SupervisorConfig(enabled=False),
        evaluators=[EvaluatorConfig(name="answer", command=f"{sys.executable} -c \"{evaluator_script}\"")],
    )
    orchestrator = Orchestrator(config)
    try:
        summary = orchestrator.run("create answer.txt containing good")
    finally:
        orchestrator.close()

    assert summary.status == "accepted"
    assert summary.best_score == 1.0
    assert git(repo, "rev-parse", summary.result_branch) == summary.best_commit
    assert git(repo, "show", f"{summary.best_commit}:answer.txt") == "good"
