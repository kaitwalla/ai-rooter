from __future__ import annotations

import uuid
from dataclasses import dataclass

from .config import AVOConfig
from .evaluator import evaluate_all
from .gitops import GitRepo
from .models import Candidate, EvaluationResult
from .store import Store
from .supervisor import Supervisor
from .worker import make_worker


@dataclass(slots=True)
class RunSummary:
    run_id: str
    status: str
    best_score: float
    best_commit: str
    result_branch: str
    iterations: int

    def to_dict(self) -> dict[str, object]:
        return {
            "run_id": self.run_id,
            "status": self.status,
            "best_score": self.best_score,
            "best_commit": self.best_commit,
            "result_branch": self.result_branch,
            "iterations": self.iterations,
        }


class Orchestrator:
    def __init__(self, config: AVOConfig):
        config.validate()
        self.config = config
        self.config.state_path.mkdir(parents=True, exist_ok=True)
        self.store = Store(self.config.state_path / "state.sqlite3")
        self.git = GitRepo(self.config.repo_path, self.config.state_path / "worktrees")
        self.worker = make_worker(self.config.worker)
        self.supervisor = Supervisor(self.config.supervisor, self.config.worker)

    def close(self) -> None:
        self.store.close()

    def _evaluation_memory(self, evaluations: list[EvaluationResult]) -> str:
        chunks = []
        for ev in evaluations:
            detail = ev.summary or ev.stderr or ev.stdout
            detail = detail.strip().replace("\n", " ")[:1000]
            chunks.append(f"{ev.name}: score={ev.score:.3f}; {detail}")
        return " | ".join(chunks)

    def _prompt(
        self,
        objective: str,
        run_id: str,
        iteration: int,
        best_score: float,
        supervisor_directive: str | None,
    ) -> str:
        recent = self.store.recent_candidates(run_id, self.config.memory_window)
        memories = self.store.recent_memories(run_id, self.config.memory_window)
        lines = [
            f"OBJECTIVE\n{objective}",
            f"\nITERATION\n{iteration} of {self.config.max_iterations}",
            f"\nCURRENT BEST SCORE\n{best_score:.4f}",
        ]
        if recent:
            lines.append("\nRECENT TRAJECTORY")
            for row in recent:
                text = (row["worker_output"] or row["worker_error"] or "").strip().replace("\n", " ")
                lines.append(
                    f"- i{row['iteration']}: score={row['score']:.4f}, improved={bool(row['improved'])}; "
                    f"worker note: {text[:800]}"
                )
        if memories:
            lines.append("\nEPISODIC MEMORY")
            for row in memories:
                lines.append(f"- [{row['kind']}] {row['content'][:1400]}")
        if supervisor_directive:
            lines.append(f"\nSUPERVISOR DIRECTIVE\n{supervisor_directive}")
        lines.append(
            "\nTASK\nInspect the workspace, choose the highest-leverage next change, implement it, and run relevant "
            "checks. Preserve useful existing work. Do not only write a plan or explanation."
        )
        return "\n".join(lines)

    def _baseline(self, run_id: str, base_commit: str) -> tuple[float, list[EvaluationResult]]:
        wt = self.git.add_worktree(run_id, "baseline", base_commit, branch=None)
        try:
            return evaluate_all(wt.path, self.config.evaluators)
        finally:
            self.git.remove_worktree(wt)

    def run(self, objective: str) -> RunSummary:
        self.git.validate(allow_dirty=self.config.allow_dirty_repo)
        run_id = uuid.uuid4().hex[:12]
        base_commit = self.git.head()
        baseline_score, baseline_evals = self._baseline(run_id, base_commit)
        self.store.create_run(
            run_id=run_id,
            objective=objective,
            repo_path=str(self.config.repo_path),
            base_commit=base_commit,
            best_score=baseline_score,
        )
        self.store.add_memory(
            run_id,
            0,
            "baseline",
            f"Baseline score={baseline_score:.4f}. {self._evaluation_memory(baseline_evals)}",
        )

        best_commit = base_commit
        best_score = baseline_score
        stagnant_rounds = 0
        last_supervised = -10_000
        directive: str | None = None
        iterations = 0
        status = "exhausted"

        try:
            if best_score >= self.config.acceptance_score:
                status = "accepted"
            else:
                for iteration in range(1, self.config.max_iterations + 1):
                    iterations = iteration
                    branch = f"{self.config.result_branch_prefix}/{run_id}/i-{iteration:04d}"
                    wt = self.git.add_worktree(
                        run_id, f"i-{iteration:04d}", best_commit, branch=branch
                    )
                    try:
                        prompt = self._prompt(
                            objective, run_id, iteration, best_score, directive
                        )
                        directive = None
                        worker_result = self.worker.run(prompt, wt.path)
                        commit_sha = self.git.commit_all(
                            wt, f"avo: candidate {run_id} iteration {iteration}"
                        )
                        score, evaluations = evaluate_all(wt.path, self.config.evaluators)
                        improved = score > best_score + self.config.min_improvement
                        candidate = Candidate(
                            iteration=iteration,
                            branch=branch,
                            workspace=wt.path,
                            base_commit=best_commit,
                            commit_sha=commit_sha,
                            score=score,
                            worker=worker_result,
                            evaluations=evaluations,
                            improved=improved,
                        )
                        self.store.add_candidate(run_id, candidate)
                        self.store.add_memory(
                            run_id,
                            iteration,
                            "attempt",
                            f"score={score:.4f}; improved={improved}; "
                            f"{self._evaluation_memory(evaluations)}",
                        )

                        if improved:
                            best_score = score
                            best_commit = commit_sha
                            stagnant_rounds = 0
                            self.store.update_run_best(run_id, best_commit, best_score)
                        else:
                            stagnant_rounds += 1

                        if best_score >= self.config.acceptance_score:
                            status = "accepted"
                            break

                        supervisor_due = (
                            self.config.supervisor.enabled
                            and stagnant_rounds >= self.config.supervisor.stagnation_rounds
                            and iteration - last_supervised >= self.config.supervisor.cooldown_rounds
                        )
                        if supervisor_due:
                            directive = self.supervisor.advise(
                                objective=objective,
                                run_id=run_id,
                                iteration=iteration,
                                best_score=best_score,
                                store=self.store,
                                memory_window=self.config.memory_window,
                            )
                            self.store.add_memory(
                                run_id, iteration, "supervisor", directive
                            )
                            last_supervised = iteration
                    finally:
                        if not self.config.keep_worktrees:
                            self.git.remove_worktree(wt)

            result_branch = f"{self.config.result_branch_prefix}/{run_id}/best"
            self.git.point_branch(result_branch, best_commit)
            self.store.finish_run(run_id, status)
            return RunSummary(
                run_id=run_id,
                status=status,
                best_score=best_score,
                best_commit=best_commit,
                result_branch=result_branch,
                iterations=iterations,
            )
        except Exception:
            self.store.finish_run(run_id, "failed")
            raise
