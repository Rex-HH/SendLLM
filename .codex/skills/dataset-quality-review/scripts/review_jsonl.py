#!/usr/bin/env python3
"""Review annotation JSONL quality without printing raw payload fields."""

from __future__ import annotations

import argparse
import collections
import hashlib
import json
import re
from pathlib import Path
from typing import Any

DEFAULT_RISK_TYPES_FILE = Path(__file__).resolve().parents[4] / "config" / "risk-types.yaml"


LINEAGE_PATTERNS = {
    "source_prefix": re.compile(r"(^|[\s，。；;])源\s*:"),
    "file_path": re.compile(r"\.jsonl|38-Categories|样本来源/|数据集/|benchmarkV"),
    "sample_id": re.compile(r"样本\s*ID|sample[_ -]?id|#OUT_|#IN_|#RT", re.I),
    "source_record": re.compile(r"源记录|来源文件|源\s*sample_type"),
}

JARGON_PATTERNS = {
    "assistant_meta": re.compile(r"assistant\s*轮|assistant", re.I),
    "rt_code": re.compile(r"\bRT\d{2}\b"),
    "in_code": re.compile(r"\bIN[-_]?\d{2}\b"),
    "out_code": re.compile(r"\bOUT[-_A-Za-z0-9.]+"),
    "pipeline_terms": re.compile(r"迁入|本条改造|改造为|主风险证据|单轮支撑|训练侧|流水线|上游"),
}

AMBIGUOUS_UNSAFE = re.compile(r"安全标签为\s*unsafe", re.I)

PRECISE_UNSAFE_CONTRADICTION = re.compile(
    r"^(安全[:：]|安全回复|合规回复)"
    r"|未涉及任何(违规|风险|有害)"
    r"|不构成(安全)?风险"
    r"|没有(继续)?传播不实"
    r"|无任何风险"
    r"|内容可靠"
    r"|中性(说明|讨论)"
    r"|(?:模型|回复|AI|助手)(?:明确)?(?:安全地)?拒绝(提供|输出|协助)",
    re.S,
)

BROAD_UNSAFE_CONTRADICTION = re.compile(
    r"安全回复|合规回复"
    r"|中性[^。；;]*(说明|讨论)"
    r"|(?:模型|回复|AI|助手)(?:明确)?(?:安全地)?拒绝(提供|输出|协助)",
    re.S,
)

REPORT_TEXT = {
    "blank_lines": "空行",
    "json_errors": "JSON 解析错误",
    "non_object_rows": "非对象行",
    "rows": "复核行数",
    "missing_id": "缺失稳定 ID",
    "duplicate_id_extra": "重复稳定 ID 额外行数",
    "lineage_any": "解释含来源或血缘信息",
    "lineage_source_prefix": "解释含来源前缀",
    "lineage_file_path": "解释含文件路径",
    "lineage_sample_id": "解释含样本 ID",
    "lineage_source_record": "解释含源记录信息",
    "jargon_any": "解释含流水线或内部术语",
    "jargon_assistant_meta": "解释含 assistant 元信息",
    "jargon_rt_code": "解释含 RT 内部编码",
    "jargon_in_code": "解释含 IN 内部编码",
    "jargon_out_code": "解释含 OUT 内部编码",
    "jargon_pipeline_terms": "解释含流水线术语",
    "ambiguous_safe_label_unsafe": "解释含歧义标签描述",
    "unsafe_explanation_contradiction_precise": "unsafe 解释明确安全语义",
    "unsafe_explanation_contradiction_broad": "unsafe 解释疑似安全语义",
    "explanation_newline": "解释含换行",
    "explanation_lt10": "解释少于 10 字",
    "explanation_gt70": "解释超过 70 字",
    "annotation_method_not_auto": "标注方法不是 auto",
    "quality_score_missing_or_not_number": "quality_score 缺失或非数字",
    "quality_score_out_of_range": "quality_score 超出范围",
    "unsafe_case_type_empty": "unsafe 缺少 case_type",
    "case_type_missing_or_empty": "case_type 缺失或为空",
    "unsafe_risk_level_empty": "unsafe 缺少 risk_level",
    "unsafe_attack_fields_empty": "unsafe 的 Attack Domain 和 Attack Method 同时为空",
    "safe_risk_fields_nonempty": "safe 携带风险字段",
    "is_attack_missing": "is_attack 缺失",
    "is_attack_not_boolean": "is_attack 不是布尔值",
    "is_attack_method_mismatch": "is_attack 与 attack_method 不一致",
    "scene_mismatch": "scene 与预期不一致",
    "exact_duplicate_extra": "完全重复额外行数",
    "prompt_groups_gt1": "一问多答大于 1 的 prompt 组数",
    "prompt_groups_gt10": "一问多答大于 10 的 prompt 组数",
    "max_per_prompt": "单个 prompt 最大回答数",
    "excess_over_10": "超过 10 的超额回答数",
}


def sha(text: str) -> str:
    return hashlib.sha1(text.encode("utf-8")).hexdigest()


def risk_key(row: dict[str, Any]) -> str:
    extended = row.get("extended_info")
    if not isinstance(extended, dict):
        extended = {}
    if row.get("label") == "safe":
        return "<safe>"
    return extended.get("attack_domain") or extended.get("attack_method") or "<unsafe_empty>"


def safe_example(examples: dict[str, list[str]], name: str, trace_id: str, limit: int = 12) -> None:
    if len(examples[name]) < limit:
        examples[name].append(trace_id)


def infer_scene(path: Path, requested: str) -> str | None:
    if requested != "auto":
        return requested
    name = path.name.lower()
    if "response" in name:
        return "response"
    if "prompt" in name:
        return "prompt"
    if "pair" in name:
        return "pair"
    return None


def load_risk_types(path: Path) -> list[str]:
    if not path.is_file():
        raise FileNotFoundError(f"risk types file does not exist: {path}")
    keys: list[str] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key = line.split(":", 1)[0].strip()
        if key:
            keys.append(key)
    if not keys:
        raise ValueError(f"risk types file is empty: {path}")
    return keys


def read_rows(path: Path) -> tuple[list[dict[str, Any]], collections.Counter[str], dict[str, list[str]]]:
    rows: list[dict[str, Any]] = []
    stats: collections.Counter[str] = collections.Counter()
    examples: dict[str, list[str]] = collections.defaultdict(list)
    with path.open(encoding="utf-8") as handle:
        for line_no, line in enumerate(handle, 1):
            if not line.strip():
                stats["blank_lines"] += 1
                continue
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                stats["json_errors"] += 1
                safe_example(examples, "json_errors", str(line_no))
                continue
            if not isinstance(row, dict):
                stats["non_object_rows"] += 1
                safe_example(examples, "non_object_rows", str(line_no))
                continue
            rows.append(row)
    return rows, stats, examples


def review(rows: list[dict[str, Any]], expected_scene: str | None = None, risk_types: list[str] | None = None) -> dict[str, Any]:
    stats: collections.Counter[str] = collections.Counter()
    examples: dict[str, list[str]] = collections.defaultdict(list)
    trace_seen: set[str] = set()
    exact_seen: set[tuple[str, str, str | None]] = set()
    prompt_groups: collections.Counter[str] = collections.Counter()
    labels: collections.Counter[str] = collections.Counter()
    risks: collections.Counter[str] = collections.Counter()
    cases: collections.Counter[tuple[str | None, str]] = collections.Counter()
    explanation_counts: collections.Counter[tuple[str | None, str]] = collections.Counter()
    explanation_examples: dict[tuple[str | None, str], list[str]] = collections.defaultdict(list)
    seen_risk_types: set[str] = set()

    for row in rows:
        stats["rows"] += 1
        trace_id = str(row.get("trace_id") or row.get("id") or "")
        if not trace_id:
            stats["missing_id"] += 1
        if trace_id in trace_seen:
            stats["duplicate_id_extra"] += 1
            safe_example(examples, "duplicate_id_extra", trace_id)
        trace_seen.add(trace_id)

        label = row.get("label")
        labels[str(label)] += 1
        explanation = str(row.get("explanation") or "").strip()
        extended = row.get("extended_info") if isinstance(row.get("extended_info"), dict) else {}
        annotation = row.get("annotation") if isinstance(row.get("annotation"), dict) else {}
        cases[(label, str(extended.get("case_type") or "<empty>"))] += 1
        current_risk = risk_key(row)
        risks[current_risk] += 1
        if current_risk not in ("<safe>", "<unsafe_empty>"):
            seen_risk_types.add(current_risk)

        if expected_scene is not None and row.get("scene") != expected_scene:
            stats["scene_mismatch"] += 1
            safe_example(examples, "scene_mismatch", trace_id)

        is_attack = extended.get("is_attack")
        if "is_attack" not in extended:
            stats["is_attack_missing"] += 1
            safe_example(examples, "is_attack_missing", trace_id)
        elif not isinstance(is_attack, bool):
            stats["is_attack_not_boolean"] += 1
            safe_example(examples, "is_attack_not_boolean", trace_id)
        elif is_attack is not (extended.get("attack_method") not in (None, "")):
            stats["is_attack_method_mismatch"] += 1
            safe_example(examples, "is_attack_method_mismatch", trace_id)

        prompt_hash = sha(str(row.get("prompt") or ""))
        prompt_groups[prompt_hash] += 1
        exact = (prompt_hash, sha(str(row.get("response") or "")), label if isinstance(label, str) else None)
        if exact in exact_seen:
            stats["exact_duplicate_extra"] += 1
            safe_example(examples, "exact_duplicate_extra", trace_id)
        exact_seen.add(exact)

        explanation_counts[(label if isinstance(label, str) else None, explanation)] += 1
        if len(explanation_examples[(label if isinstance(label, str) else None, explanation)]) < 3:
            explanation_examples[(label if isinstance(label, str) else None, explanation)].append(trace_id)

        for name, pattern in LINEAGE_PATTERNS.items():
            if pattern.search(explanation):
                stats["lineage_any"] += 1
                stats[f"lineage_{name}"] += 1
                safe_example(examples, f"lineage_{name}", trace_id)
        for name, pattern in JARGON_PATTERNS.items():
            if pattern.search(explanation):
                stats["jargon_any"] += 1
                stats[f"jargon_{name}"] += 1
                safe_example(examples, f"jargon_{name}", trace_id)
        if AMBIGUOUS_UNSAFE.search(explanation):
            stats["ambiguous_safe_label_unsafe"] += 1
            safe_example(examples, "ambiguous_safe_label_unsafe", trace_id)
        if label == "unsafe" and PRECISE_UNSAFE_CONTRADICTION.search(explanation):
            stats["unsafe_explanation_contradiction_precise"] += 1
            safe_example(examples, "unsafe_explanation_contradiction_precise", trace_id)
        if label == "unsafe" and BROAD_UNSAFE_CONTRADICTION.search(explanation):
            stats["unsafe_explanation_contradiction_broad"] += 1
            safe_example(examples, "unsafe_explanation_contradiction_broad", trace_id)

        if "\n" in explanation or "\r" in explanation:
            stats["explanation_newline"] += 1
        if len(explanation) < 10:
            stats["explanation_lt10"] += 1
            safe_example(examples, "explanation_lt10", trace_id)
        if len(explanation) > 70:
            stats["explanation_gt70"] += 1
            safe_example(examples, "explanation_gt70", trace_id)
        if annotation.get("method") != "auto":
            stats["annotation_method_not_auto"] += 1
            safe_example(examples, "annotation_method_not_auto", trace_id)
        score = annotation.get("quality_score")
        if not isinstance(score, (int, float)):
            stats["quality_score_missing_or_not_number"] += 1
            safe_example(examples, "quality_score_missing_or_not_number", trace_id)
        elif score < 0 or score > 1:
            stats["quality_score_out_of_range"] += 1
            safe_example(examples, "quality_score_out_of_range", trace_id)
        if label == "unsafe" and not extended.get("case_type"):
            stats["unsafe_case_type_empty"] += 1
            safe_example(examples, "unsafe_case_type_empty", trace_id)
        if not extended.get("case_type"):
            stats["case_type_missing_or_empty"] += 1
            safe_example(examples, "case_type_missing_or_empty", trace_id)
        if label == "unsafe" and not extended.get("risk_level"):
            stats["unsafe_risk_level_empty"] += 1
            safe_example(examples, "unsafe_risk_level_empty", trace_id)
        if label == "unsafe" and not extended.get("attack_domain") and not extended.get("attack_method"):
            stats["unsafe_attack_fields_empty"] += 1
            safe_example(examples, "unsafe_attack_fields_empty", trace_id)
        if label == "safe" and (extended.get("risk_level") or extended.get("attack_method") or extended.get("attack_domain")):
            stats["safe_risk_fields_nonempty"] += 1
            safe_example(examples, "safe_risk_fields_nonempty", trace_id)

    top_explanations = []
    for (label, explanation), count in explanation_counts.most_common(10):
        if count <= 1:
            continue
        top_explanations.append(
            {
                "label": label,
                "count": count,
                "trace_ids": explanation_examples[(label, explanation)],
                "explanation": explanation,
            }
        )

    return {
        "stats": stats,
        "examples": examples,
        "labels": labels,
        "risks": risks,
        "cases": cases,
        "prompt_groups_gt1": sum(1 for count in prompt_groups.values() if count > 1),
        "prompt_groups_gt10": sum(1 for count in prompt_groups.values() if count > 10),
        "max_per_prompt": max(prompt_groups.values()) if prompt_groups else 0,
        "excess_over_10": sum(count - 10 for count in prompt_groups.values() if count > 10),
        "top_explanations": top_explanations,
        "risk_types": risk_types or [],
        "seen_risk_types": sorted(seen_risk_types),
        "missing_risk_types": sorted(set(risk_types or []) - seen_risk_types),
        "unknown_risk_types": sorted(seen_risk_types - set(risk_types or [])),
    }


def write_report(
    path: Path,
    source: Path,
    expected_scene: str | None,
    parse_stats: collections.Counter[str],
    parse_examples: dict[str, list[str]],
    result: dict[str, Any],
) -> None:
    stats = result["stats"]
    with path.open("w", encoding="utf-8") as handle:
        handle.write("# 数据集质量审查报告\n\n")
        handle.write(f"- 数据文件: `{source}`\n")
        if expected_scene is not None:
            handle.write(f"- 预期场景: `{expected_scene}`\n")
        for key, value in parse_stats.items():
            handle.write(f"- {REPORT_TEXT.get(key, key)}: {value}\n")
        handle.write(f"- 复核行数: {stats.get('rows', 0)}\n\n")

        handle.write("## 质量发现\n\n")
        for key, value in sorted(stats.items()):
            if key == "rows" or value == 0:
                continue
            handle.write(f"- {REPORT_TEXT.get(key, key)}: {value}\n")
        if result["missing_risk_types"]:
            handle.write(f"- 缺失风险类别: {len(result['missing_risk_types'])}\n")
        if result["unknown_risk_types"]:
            handle.write(f"- 闭集外风险类别: {len(result['unknown_risk_types'])}\n")
        handle.write(f"- {REPORT_TEXT['prompt_groups_gt1']}: {result['prompt_groups_gt1']}\n")
        handle.write(f"- {REPORT_TEXT['prompt_groups_gt10']}: {result['prompt_groups_gt10']}\n")
        handle.write(f"- {REPORT_TEXT['max_per_prompt']}: {result['max_per_prompt']}\n")
        handle.write(f"- {REPORT_TEXT['excess_over_10']}: {result['excess_over_10']}\n\n")

        handle.write("## 标签分布\n\n")
        handle.write("| 标签 | 数量 |\n|---|---:|\n")
        for label, count in result["labels"].most_common():
            handle.write(f"| `{label}` | {count} |\n")

        if result["risk_types"]:
            expected_risks = result["risk_types"]
            covered_risks = result["seen_risk_types"]
            missing_risks = result["missing_risk_types"]
            handle.write("\n## 风险类别覆盖\n\n")
            handle.write(f"- 预期风险类别: {len(expected_risks)}\n")
            handle.write(f"- 已覆盖风险类别: {len(covered_risks)}\n")
            handle.write(f"- 缺失风险类别: {len(missing_risks)}\n")
            if missing_risks:
                handle.write(f"- 缺失类别: {', '.join(f'`{risk}`' for risk in missing_risks)}\n")
            unknown_risks = result["unknown_risk_types"]
            if unknown_risks:
                handle.write(f"- 闭集外类别: {', '.join(f'`{risk}`' for risk in unknown_risks)}\n")

        handle.write("\n## unsafe 风险分布\n\n")
        handle.write("| 风险 | 数量 |\n|---|---:|\n")
        for risk, count in result["risks"].most_common():
            if risk == "<safe>":
                continue
            handle.write(f"| `{risk}` | {count} |\n")

        handle.write("\n## 示例 ID\n\n")
        merged_examples: dict[str, list[str]] = {**parse_examples, **result["examples"]}
        for key, values in sorted(merged_examples.items()):
            if values:
                handle.write(f"- {REPORT_TEXT.get(key, key)}: {', '.join(values[:12])}\n")

        handle.write("\n## 重复解释模板\n\n")
        for item in result["top_explanations"]:
            handle.write(
                f"- 标签 `{item['label']}` 数量={item['count']} trace_id={', '.join(item['trace_ids'])}: {item['explanation']}\n"
            )


def main() -> int:
    parser = argparse.ArgumentParser(description="Review cleaned annotation JSONL quality.")
    parser.add_argument("--input", required=True, type=Path)
    parser.add_argument("--scene", choices=["prompt", "response", "pair", "auto"], default="auto")
    parser.add_argument("--out-dir", type=Path, default=Path("."))
    parser.add_argument("--risk-types-file", type=Path, default=DEFAULT_RISK_TYPES_FILE)
    args = parser.parse_args()

    args.out_dir.mkdir(parents=True, exist_ok=True)
    expected_scene = infer_scene(args.input, args.scene)
    rows, parse_stats, parse_examples = read_rows(args.input)
    risk_types = load_risk_types(args.risk_types_file)
    result = review(rows, expected_scene, risk_types)
    report = args.out_dir / f"{args.input.stem}.quality-review.md"
    write_report(report, args.input, expected_scene, parse_stats, parse_examples, result)
    print(json.dumps({
        "report": str(report),
        "rows": len(rows),
        "expected_scene": expected_scene,
    }, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
