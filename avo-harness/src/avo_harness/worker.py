from __future__ import annotations

import asyncio
import os
import shlex
import subprocess
import tempfile
from pathlib import Path
from typing import Protocol

from .config import WorkerConfig
from .models import WorkerResult


class Worker(Protocol):
    def run(self, prompt: str, workspace: Path) -> WorkerResult: ...


class CommandWorker:
    def __init__(self, config: WorkerConfig):
        self.config = config

    def run(self, prompt: str, workspace: Path) -> WorkerResult:
        env = os.environ.copy()
        env.update(self.config.env)
        env["AVO_WORKSPACE"] = str(workspace)
        env["AVO_PROMPT"] = prompt
        prompt_path = None
        try:
            with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as handle:
                handle.write(prompt)
                prompt_path = handle.name
            command = [
                part.replace("{workspace}", str(workspace))
                .replace("{prompt_file}", prompt_path)
                .replace("{prompt}", prompt)
                for part in self.config.command
            ]
            uses_prompt_placeholder = any(
                token in part for part in self.config.command for token in ("{prompt}", "{prompt_file}")
            )
            proc = subprocess.run(
                command,
                cwd=workspace,
                env=env,
                input=None if uses_prompt_placeholder else prompt,
                text=True,
                capture_output=True,
                timeout=self.config.timeout_seconds,
            )
            return WorkerResult(
                success=proc.returncode == 0,
                output=proc.stdout[-50000:],
                error=proc.stderr[-50000:],
                metadata={"return_code": proc.returncode, "command": shlex.join(command)},
            )
        except subprocess.TimeoutExpired as exc:
            return WorkerResult(
                success=False,
                output=(exc.stdout or "")[-50000:] if isinstance(exc.stdout, str) else "",
                error=f"worker timed out after {self.config.timeout_seconds}s",
            )
        finally:
            if prompt_path:
                try:
                    os.unlink(prompt_path)
                except FileNotFoundError:
                    pass


class NeMoWorker:
    def __init__(self, config: WorkerConfig):
        self.config = config

    def run(self, prompt: str, workspace: Path) -> WorkerResult:
        try:
            return asyncio.run(self._run(prompt, workspace))
        except Exception as exc:  # normalized at the harness boundary
            return WorkerResult(success=False, error=f"NeMo Fabric worker failed: {exc}")

    async def _run(self, prompt: str, workspace: Path) -> WorkerResult:
        try:
            from nemo_fabric import (
                EnvironmentConfig,
                Fabric,
                FabricConfig,
                HarnessConfig,
                InstructionConfig,
                InstructionsConfig,
                MetadataConfig,
                ModelConfig,
                RuntimeConfig,
            )
        except ImportError as exc:
            raise RuntimeError(
                "NeMo Fabric is not installed. Install this project with the 'nemo' extra."
            ) from exc

        models = {}
        if self.config.model:
            model_kwargs = {
                "provider": self.config.provider or "nvidia",
                "model": self.config.model,
            }
            if self.config.api_key_env:
                model_kwargs["api_key_env"] = self.config.api_key_env
            if self.config.base_url:
                model_kwargs["base_url"] = self.config.base_url
            models["default"] = ModelConfig(**model_kwargs)

        config = FabricConfig(
            metadata=MetadataConfig(name="avo-worker"),
            harness=HarnessConfig(adapter_id=self.config.adapter_id),
            instructions=InstructionsConfig(
                system=InstructionConfig(content=self.config.system_prompt, mode="replace")
            ),
            runtime=RuntimeConfig(
                max_turns=self.config.max_turns,
                timeout_seconds=self.config.timeout_seconds,
            ),
            environment=EnvironmentConfig(
                provider="local",
                workspace=str(workspace),
                env=self.config.env,
            ),
            models=models,
        )
        result = await Fabric().run(config, input=prompt)
        output_obj = getattr(result, "output", None)
        response = getattr(output_obj, "response", "") if output_obj is not None else ""
        error_obj = getattr(result, "error", None)
        status = str(getattr(result, "status", "unknown"))
        return WorkerResult(
            success=error_obj is None,
            output=str(response or ""),
            error="" if error_obj is None else str(error_obj),
            metadata={"status": status, "adapter_id": self.config.adapter_id},
        )


def make_worker(config: WorkerConfig) -> Worker:
    if config.backend == "command":
        return CommandWorker(config)
    if config.backend == "nemo":
        return NeMoWorker(config)
    raise ValueError(f"unknown worker backend: {config.backend}")
