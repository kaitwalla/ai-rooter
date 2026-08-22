from __future__ import annotations

import sqlite3
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable

from .models import Candidate, EvaluationResult


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


class Store:
    def __init__(self, path: Path):
        path.parent.mkdir(parents=True, exist_ok=True)
        self.path = path
        self.db = sqlite3.connect(path)
        self.db.row_factory = sqlite3.Row
        self._init_schema()

    def close(self) -> None:
        self.db.close()

    def _init_schema(self) -> None:
        self.db.executescript(
            """
            PRAGMA journal_mode=WAL;
            CREATE TABLE IF NOT EXISTS runs (
                id TEXT PRIMARY KEY,
                objective TEXT NOT NULL,
                repo_path TEXT NOT NULL,
                base_commit TEXT NOT NULL,
                best_commit TEXT NOT NULL,
                best_score REAL NOT NULL,
                status TEXT NOT NULL,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS candidates (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                run_id TEXT NOT NULL,
                iteration INTEGER NOT NULL,
                branch TEXT NOT NULL,
                base_commit TEXT NOT NULL,
                commit_sha TEXT NOT NULL,
                score REAL NOT NULL,
                worker_success INTEGER NOT NULL,
                worker_output TEXT NOT NULL,
                worker_error TEXT NOT NULL,
                improved INTEGER NOT NULL,
                created_at TEXT NOT NULL,
                FOREIGN KEY(run_id) REFERENCES runs(id)
            );
            CREATE TABLE IF NOT EXISTS evaluations (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                candidate_id INTEGER NOT NULL,
                name TEXT NOT NULL,
                score REAL NOT NULL,
                weight REAL NOT NULL,
                passed INTEGER NOT NULL,
                return_code INTEGER NOT NULL,
                summary TEXT NOT NULL,
                stdout TEXT NOT NULL,
                stderr TEXT NOT NULL,
                FOREIGN KEY(candidate_id) REFERENCES candidates(id)
            );
            CREATE TABLE IF NOT EXISTS memories (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                run_id TEXT NOT NULL,
                iteration INTEGER NOT NULL,
                kind TEXT NOT NULL,
                content TEXT NOT NULL,
                created_at TEXT NOT NULL,
                FOREIGN KEY(run_id) REFERENCES runs(id)
            );
            CREATE INDEX IF NOT EXISTS idx_candidates_run_iteration
              ON candidates(run_id, iteration DESC);
            CREATE INDEX IF NOT EXISTS idx_memories_run_iteration
              ON memories(run_id, iteration DESC);
            """
        )
        self.db.commit()

    def create_run(
        self,
        run_id: str,
        objective: str,
        repo_path: str,
        base_commit: str,
        best_score: float,
    ) -> None:
        now = _now()
        self.db.execute(
            """INSERT INTO runs
               (id, objective, repo_path, base_commit, best_commit, best_score, status, created_at, updated_at)
               VALUES (?, ?, ?, ?, ?, ?, 'running', ?, ?)""",
            (run_id, objective, repo_path, base_commit, base_commit, best_score, now, now),
        )
        self.db.commit()

    def update_run_best(self, run_id: str, commit_sha: str, score: float) -> None:
        self.db.execute(
            "UPDATE runs SET best_commit=?, best_score=?, updated_at=? WHERE id=?",
            (commit_sha, score, _now(), run_id),
        )
        self.db.commit()

    def finish_run(self, run_id: str, status: str) -> None:
        self.db.execute(
            "UPDATE runs SET status=?, updated_at=? WHERE id=?", (status, _now(), run_id)
        )
        self.db.commit()

    def add_candidate(self, run_id: str, candidate: Candidate) -> int:
        cursor = self.db.execute(
            """INSERT INTO candidates
               (run_id, iteration, branch, base_commit, commit_sha, score,
                worker_success, worker_output, worker_error, improved, created_at)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                run_id,
                candidate.iteration,
                candidate.branch,
                candidate.base_commit,
                candidate.commit_sha,
                candidate.score,
                int(candidate.worker.success),
                candidate.worker.output,
                candidate.worker.error,
                int(candidate.improved),
                _now(),
            ),
        )
        candidate_id = int(cursor.lastrowid)
        self._add_evaluations(candidate_id, candidate.evaluations)
        self.db.commit()
        return candidate_id

    def _add_evaluations(
        self, candidate_id: int, evaluations: Iterable[EvaluationResult]
    ) -> None:
        self.db.executemany(
            """INSERT INTO evaluations
               (candidate_id, name, score, weight, passed, return_code, summary, stdout, stderr)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            [
                (
                    candidate_id,
                    ev.name,
                    ev.score,
                    ev.weight,
                    int(ev.passed),
                    ev.return_code,
                    ev.summary,
                    ev.stdout,
                    ev.stderr,
                )
                for ev in evaluations
            ],
        )

    def add_memory(self, run_id: str, iteration: int, kind: str, content: str) -> None:
        self.db.execute(
            "INSERT INTO memories (run_id, iteration, kind, content, created_at) VALUES (?, ?, ?, ?, ?)",
            (run_id, iteration, kind, content, _now()),
        )
        self.db.commit()

    def recent_candidates(self, run_id: str, limit: int = 6) -> list[sqlite3.Row]:
        return list(
            self.db.execute(
                """SELECT * FROM candidates WHERE run_id=?
                   ORDER BY iteration DESC LIMIT ?""",
                (run_id, limit),
            ).fetchall()
        )[::-1]

    def recent_memories(self, run_id: str, limit: int = 6) -> list[sqlite3.Row]:
        return list(
            self.db.execute(
                """SELECT * FROM memories WHERE run_id=?
                   ORDER BY id DESC LIMIT ?""",
                (run_id, limit),
            ).fetchall()
        )[::-1]

    def get_run(self, run_id: str) -> sqlite3.Row | None:
        return self.db.execute("SELECT * FROM runs WHERE id=?", (run_id,)).fetchone()

    def latest_run(self) -> sqlite3.Row | None:
        return self.db.execute("SELECT * FROM runs ORDER BY created_at DESC LIMIT 1").fetchone()
