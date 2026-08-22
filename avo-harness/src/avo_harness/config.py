from __future__ import annotations

import json
import os
import shlex
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any


DEFAULT_SYSTEM_PROMPT = """You are the implementation worker in a long-horizon coding loop.
Work directly in the provided repository workspace. Inspect the code before changing it.
Make concrete changes that advance the objective, run relevant tests or checks, and leave
all useful modifications in the workspace. Do not merely describe what should be changed.
Use the supplied memory to avoid repeating failed approaches."""

SUPERVISOR_SYSTEM_PROMPT = """You supervise a long-horizon coding agent. You do not edit the target repository.
Study the objective, scores, evaluator feedback, and recent attempt summaries. Identify why
progress has stalled and return a concise strategic directive for the next worker. Prefer a
materially different hypothesis or decomposition over generic encouragement."""


@dataclass(slots=True)
class WorkerConfig:
    backend: str = "command"  # command | nemo
    command: list[str] = field(default_factory=list)
    adapter_id: str = "nvidia.fabric.hermes"
    provider: str | None = None
    model: str | None = None
    api_key_env: str | None = None
    base_url: str | None = None
    max_turns: int = 24
    timeout_seconds: int = 3600
    system_prompt: str = DEFAULT_SYSTEM_PROMPT
    env: dict[str, str] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, raw: dict[str, Any] | None) -> "WorkerConfig":
        raw = dict(raw or {})
        command = raw.get("command", [])
        if isinstance(command, str):
            command = shlex.split(command)
        raw["command"] = command
        return cls(**raw)


@dataclass(slots=True)
class EvaluatorConfig:
    name: str
    command: str
    weight: float = 1.0
    timeout_seconds: int = 900
    pass_score: float = 1.0

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "EvaluatorConfig":
        return cls(**raw)


@dataclass(slots=True)
class SupervisorConfig:
    enabled: bool = True
    stagnation_rounds: int = 2
    cooldown_rounds: int = 2
    worker: WorkerConfig | None = None

    @classmethod
    def from_dict(cls, raw: dict[str, Any] | None) -> "SupervisorConfig":
        raw = dict(raw or {})
        if "worker" in raw and raw["worker"] is not None:
            raw["worker"] = WorkerConfig.from_dict(raw["worker"])
        return cls(**raw)


@dataclass(slots=True)
class AVOConfig:
    repo: str
    worker: WorkerConfig
    evaluators: list[EvaluatorConfig]
    supervisor: SupervisorConfig = field(default_factory=SupervisorConfig)
    state_dir: str = "~/.local/state/avo-harness"
    max_iterations: int = 12
    acceptance_score: float = 1.0
    min_improvement: float = 0.001
    memory_window: int = 6
    keep_worktrees: bool = False
    allow_dirty_repo: bool = False
    result_branch_prefix: str = "avo"

    @property
    def repo_path(self) -> Path:
        return Path(os.path.expanduser(self.repo)).resolve()

    @property
    def state_path(self) -> Path:
        return Path(os.path.expanduser(self.state_dir)).resolve()

    def validate(self) -> None:
        if self.max_iterations < 1:
            raise ValueError("max_iterations must be >= 1")
        if not self.evaluators:
            raise ValueError("at least one evaluator is required")
        if self.worker.backend not in {"command", "nemo"}:
            raise ValueError("worker.backend must be 'command' or 'nemo'")
        if self.worker.backend == "command" and not self.worker.command:
            raise ValueError("command worker requires worker.command")
        if self.supervisor.stagnation_rounds < 1:
            raise ValueError("supervisor.stagnation_rounds must be >= 1")
        for ev in self.evaluators:
            if ev.weight <= 0:
                raise ValueError(f"evaluator {ev.name!r} weight must be > 0")

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "AVOConfig":
        raw = dict(raw)
        raw["worker"] = WorkerConfig.from_dict(raw.get("worker"))
        raw["evaluators"] = [EvaluatorConfig.from_dict(x) for x in raw.get("evaluators", [])]
        raw["supervisor"] = SupervisorConfig.from_dict(raw.get("supervisor"))
        config = cls(**raw)
        config.validate()
        return config

    @classmethod
    def load(cls, path: str | Path) -> "AVOConfig":
        with open(path, "r", encoding="utf-8") as handle:
            return cls.from_dict(json.load(handle))

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


def example_config(repo: str = ".") -> dict[str, Any]:
    return {
        "repo": repo,
        "state_dir": "~/.local/state/avo-harness",
        "max_iterations": 12,
        "acceptance_score": 1.0,
        "min_improvement": 0.001,
        "memory_window": 6,
        "keep_worktrees": False,
        "worker": {
            "backend": "nemo",
            "adapter_id": "nvidia.fabric.hermes",
            "provider": "nvidia",
            "model": "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning",
            "api_key_env": "NVIDIA_API_KEY",
            "base_url": "https://integrate.api.nvidia.com/v1",
            "max_turns": 24,
            "timeout_seconds": 3600,
        },
        "supervisor": {
            "enabled": True,
            "stagnation_rounds": 2,
            "cooldown_rounds": 2,
        },
        "evaluators": [
            {
                "name": "tests",
                "command": "pytest -q",
                "weight": 1.0,
                "timeout_seconds": 900,
                "pass_score": 1.0,
            }
        ],
    }
