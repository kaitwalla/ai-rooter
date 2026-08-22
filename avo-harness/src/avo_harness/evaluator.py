from __future__ import annotations

import json
import subprocess
from pathlib import Path

from .config import EvaluatorConfig
from .models import EvaluationResult


def _parse_payload(stdout: str, return_code: int) -> tuple[float, str]:
    lines = [line.strip() for line in stdout.splitlines() if line.strip()]
    if lines:
        try:
            payload = json.loads(lines[-1])
            if isinstance(payload, dict) and "score" in payload:
                score = max(0.0, min(1.0, float(payload["score"])))
                return score, str(payload.get("summary", ""))
        except (ValueError, TypeError, json.JSONDecodeError):
            pass
    return (1.0 if return_code == 0 else 0.0), ""


def evaluate_one(workspace: Path, spec: EvaluatorConfig) -> EvaluationResult:
    try:
        proc = subprocess.run(
            spec.command,
            cwd=workspace,
            shell=True,
            text=True,
            capture_output=True,
            timeout=spec.timeout_seconds,
        )
        score, summary = _parse_payload(proc.stdout, proc.returncode)
        return EvaluationResult(
            name=spec.name,
            score=score,
            weight=spec.weight,
            passed=score >= spec.pass_score,
            return_code=proc.returncode,
            summary=summary,
            stdout=proc.stdout[-20000:],
            stderr=proc.stderr[-20000:],
        )
    except subprocess.TimeoutExpired as exc:
        return EvaluationResult(
            name=spec.name,
            score=0.0,
            weight=spec.weight,
            passed=False,
            return_code=124,
            summary=f"timed out after {spec.timeout_seconds}s",
            stdout=(exc.stdout or "")[-20000:] if isinstance(exc.stdout, str) else "",
            stderr=(exc.stderr or "")[-20000:] if isinstance(exc.stderr, str) else "",
        )


def evaluate_all(workspace: Path, specs: list[EvaluatorConfig]) -> tuple[float, list[EvaluationResult]]:
    results = [evaluate_one(workspace, spec) for spec in specs]
    total_weight = sum(item.weight for item in results)
    score = sum(item.score * item.weight for item in results) / total_weight
    return score, results
