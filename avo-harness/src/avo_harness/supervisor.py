from __future__ import annotations

import tempfile
from dataclasses import replace
from pathlib import Path

from .config import SUPERVISOR_SYSTEM_PROMPT, SupervisorConfig, WorkerConfig
from .store import Store
from .worker import make_worker


class Supervisor:
    def __init__(self, config: SupervisorConfig, primary_worker: WorkerConfig):
        self.config = config
        source = config.worker or primary_worker
        worker_config = replace(
            source,
            command=list(source.command),
            env=dict(source.env),
            system_prompt=SUPERVISOR_SYSTEM_PROMPT,
        )
        self.worker = make_worker(worker_config)

    def advise(
        self,
        objective: str,
        run_id: str,
        iteration: int,
        best_score: float,
        store: Store,
        memory_window: int,
    ) -> str:
        recent = store.recent_candidates(run_id, memory_window)
        lines = [
            f"Objective: {objective}",
            f"Current best score: {best_score:.4f}",
            "Recent attempts:",
        ]
        for row in recent:
            worker_text = (row["worker_output"] or row["worker_error"] or "").strip().replace("\n", " ")
            lines.append(
                f"- iteration {row['iteration']}: score={row['score']:.4f}, "
                f"improved={bool(row['improved'])}, worker={worker_text[:700]}"
            )
        lines.append(
            "Return one concrete directive for the next worker. Call out an approach to stop repeating, "
            "what evidence to inspect next, and the next hypothesis or decomposition to try."
        )
        with tempfile.TemporaryDirectory(prefix="avo-supervisor-") as tmp:
            result = self.worker.run("\n".join(lines), Path(tmp))
        if result.success and result.output.strip():
            return result.output.strip()
        return (
            "Progress has stalled. Re-read evaluator failures and the relevant implementation before editing. "
            "List three plausible root causes internally, choose one not already attempted, make the smallest "
            "testable change for that hypothesis, and run the evaluator-facing checks before broad refactors."
        )
