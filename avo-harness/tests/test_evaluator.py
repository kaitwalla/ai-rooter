from pathlib import Path

from avo_harness.config import EvaluatorConfig
from avo_harness.evaluator import evaluate_all, evaluate_one


def test_json_score_protocol(tmp_path: Path) -> None:
    spec = EvaluatorConfig(
        name="quality",
        command="python -c 'print(\"{\\\"score\\\": 0.75, \\\"summary\\\": \\\"better\\\"}\")'",
    )
    result = evaluate_one(tmp_path, spec)
    assert result.score == 0.75
    assert result.summary == "better"


def test_weighted_score(tmp_path: Path) -> None:
    specs = [
        EvaluatorConfig(name="a", command="python -c 'print(\"{\\\"score\\\": 1}\")'", weight=1),
        EvaluatorConfig(name="b", command="python -c 'print(\"{\\\"score\\\": 0}\")'", weight=3),
    ]
    score, _ = evaluate_all(tmp_path, specs)
    assert score == 0.25
