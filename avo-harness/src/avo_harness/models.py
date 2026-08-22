from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass(slots=True)
class WorkerResult:
    success: bool
    output: str = ""
    error: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass(slots=True)
class EvaluationResult:
    name: str
    score: float
    weight: float
    passed: bool
    return_code: int
    summary: str = ""
    stdout: str = ""
    stderr: str = ""


@dataclass(slots=True)
class Candidate:
    iteration: int
    branch: str
    workspace: Path
    base_commit: str
    commit_sha: str
    score: float
    worker: WorkerResult
    evaluations: list[EvaluationResult]
    improved: bool = False
