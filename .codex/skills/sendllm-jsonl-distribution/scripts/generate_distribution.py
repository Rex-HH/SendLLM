#!/usr/bin/env python3
"""Generate an aggregate distribution report for a SendLLM JSONL dataset."""

import argparse
import hashlib
import json
from collections import Counter
from pathlib import Path


SIMPLE_FIELDS = ("label", "language", "source", "split", "scene")
EXTENDED_FIELDS = (
    "attack_domain",
    "attack_method",
    "attack_scenario",
    "case_type",
    "is_attack",
    "risk_level",
)
ANNOTATION_FIELDS = ("method", "quality_score")
VALID_LANGUAGES = {"zh", "en", "other"}


def display(value, missing=False):
    if missing:
        return "<missing>"
    if value is None:
        return "<null>"
    if value == "":
        return "<empty>"
    if isinstance(value, bool):
        return str(value)
    if isinstance(value, float):
        return format(value, "g")
    return str(value)


def load_risk_types(path):
    risk_types = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition(":")
        if separator:
            risk_types[key.strip()] = value.strip()
    return risk_types


def add_cross(counter, left, right, right_missing=False):
    counter[f"{left} | {display(right, right_missing)}"] += 1


def markdown_table(counter, denominator=None):
    total = denominator if denominator is not None else sum(counter.values())
    lines = ["| 值 | 数量 | 占比 |", "|---|---:|---:|"]
    for key, count in sorted(counter.items(), key=lambda item: (-item[1], item[0])):
        percentage = count * 100 / total if total else 0
        lines.append(f"| `{key}` | {count} | {percentage:.2f}% |")
    return "\n".join(lines)


def counter_text(counter):
    if not counter:
        return "0"
    return ", ".join(f"`{key}`: {value}" for key, value in sorted(counter.items()))


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, type=Path, help="Input JSONL file")
    parser.add_argument("--output", type=Path, help="Output Markdown file")
    parser.add_argument(
        "--risk-types-file",
        type=Path,
        default=Path("config/risk-types.yaml"),
        help="Risk type YAML file used for 38-class coverage",
    )
    return parser.parse_args()


def main():
    args = parse_args()
    if not args.input.is_file():
        raise SystemExit(f"input file does not exist: {args.input}")
    output = args.output
    if output is None:
        output = args.input.with_name(f"{args.input.stem}.distribution.md")
    if output.resolve() == args.input.resolve():
        raise SystemExit("output file must be different from the input file")

    rows = 0
    blank_lines = 0
    parse_errors = 0
    missing_trace_ids = 0
    duplicate_extra_rows = 0
    invalid_prompts = 0
    safe_with_attack = 0
    unsafe_without_attack = 0
    unsafe_with_attack = 0
    trace_ids = set()
    prompt_counts = Counter()
    invalid_languages = Counter()
    missing_extended = Counter()
    null_extended = Counter()

    simple = {field: Counter() for field in SIMPLE_FIELDS}
    extended = {field: Counter() for field in EXTENDED_FIELDS}
    annotation = {field: Counter() for field in ANNOTATION_FIELDS}
    label_language = Counter()
    label_case_type = Counter()
    label_risk_level = Counter()
    label_split = Counter()
    label_attack_domain = Counter()
    unsafe_risk_domain = Counter()
    attack_domain_counts = Counter()
    attack_method_counts = Counter()
    primary_risk_counts = Counter()

    risk_types = load_risk_types(args.risk_types_file)

    with args.input.open(encoding="utf-8") as handle:
        for line in handle:
            if not line.strip():
                blank_lines += 1
                continue
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                parse_errors += 1
                continue

            rows += 1
            trace_id = row.get("trace_id")
            if trace_id in (None, ""):
                missing_trace_ids += 1
            elif trace_id in trace_ids:
                duplicate_extra_rows += 1
            else:
                trace_ids.add(trace_id)

            for field in SIMPLE_FIELDS:
                simple[field][display(row.get(field), field not in row)] += 1

            annotation_row = row.get("annotation")
            if not isinstance(annotation_row, dict):
                for field in ANNOTATION_FIELDS:
                    annotation[field]["<missing object>" if annotation_row is None else "<non-object>"] += 1
            else:
                for field in ANNOTATION_FIELDS:
                    annotation[field][display(annotation_row.get(field), field not in annotation_row)] += 1

            extended_row = row.get("extended_info")
            if not isinstance(extended_row, dict):
                marker = "<missing object>" if extended_row is None else "<non-object>"
                for field in EXTENDED_FIELDS:
                    missing_extended[field] += 1
                    extended[field][marker] += 1
            else:
                for field in EXTENDED_FIELDS:
                    if field not in extended_row:
                        missing_extended[field] += 1
                    elif extended_row[field] is None:
                        null_extended[field] += 1
                    value = extended_row.get(field)
                    if field == "attack_scenario" and value not in (None, ""):
                        extended[field]["<non-empty>"] += 1
                    else:
                        extended[field][display(value, field not in extended_row)] += 1

            if row.get("language") not in VALID_LANGUAGES:
                invalid_languages["<invalid>"] += 1

            label = display(row.get("label"), "label" not in row)
            language = row.get("language")
            split = row.get("split")
            add_cross(label_language, label, language, "language" not in row)
            add_cross(label_split, label, split, "split" not in row)

            if not isinstance(extended_row, dict):
                for counter in (label_case_type, label_risk_level, label_attack_domain):
                    counter[f"{label} | <missing object>"] += 1
            else:
                case_type = extended_row.get("case_type")
                risk_level = extended_row.get("risk_level")
                attack_domain = extended_row.get("attack_domain")
                attack_method = extended_row.get("attack_method")
                if row.get("label") == "unsafe":
                    if attack_domain not in (None, ""):
                        attack_domain_counts[attack_domain] += 1
                    if attack_method not in (None, ""):
                        attack_method_counts[attack_method] += 1
                    primary_risk = attack_method if attack_method not in (None, "") else attack_domain
                    if primary_risk not in (None, ""):
                        primary_risk_counts[primary_risk] += 1
                    if attack_domain not in (None, "") or attack_method not in (None, ""):
                        unsafe_with_attack += 1
                    else:
                        unsafe_without_attack += 1
                add_cross(label_case_type, label, case_type, "case_type" not in extended_row)
                add_cross(label_risk_level, label, risk_level, "risk_level" not in extended_row)
                add_cross(label_attack_domain, label, attack_domain, "attack_domain" not in extended_row)

                if attack_domain not in (None, "") or attack_method not in (None, ""):
                    if row.get("label") == "safe":
                        safe_with_attack += 1

                if row.get("label") == "unsafe":
                    key = f"{display(risk_level, 'risk_level' not in extended_row)} | {display(attack_domain, 'attack_domain' not in extended_row)}"
                    unsafe_risk_domain[key] += 1

            if not isinstance(extended_row, dict) and row.get("label") == "unsafe":
                unsafe_without_attack += 1

            prompt = row.get("prompt")
            if isinstance(prompt, str):
                digest = hashlib.sha256(prompt.encode("utf-8")).hexdigest()
                prompt_counts[digest] += 1
            else:
                invalid_prompts += 1

    answer_groups = Counter(prompt_counts.values())
    unique_prompts = len(prompt_counts)
    groups_over_1 = sum(count for answers, count in answer_groups.items() if answers > 1)
    groups_over_10 = sum(count for answers, count in answer_groups.items() if answers > 10)
    max_answers = max(answer_groups, default=0)
    excess_over_10 = sum(
        (answers - 10) * group_count
        for answers, group_count in answer_groups.items()
        if answers > 10
    )
    covered_risks = sum(1 for risk in risk_types if primary_risk_counts.get(risk, 0) > 0)
    missing_risks = [risk for risk in risk_types if primary_risk_counts.get(risk, 0) == 0]

    report = [
        f"# {args.input.name} 分布统计",
        "",
        f"- 文件: `{args.input}`",
        f"- 总行数: {rows}",
        f"- 空行: {blank_lines}",
        f"- JSON 解析错误: {parse_errors}",
        f"- 缺失 trace_id: {missing_trace_ids}",
        f"- 唯一 trace_id: {len(trace_ids)}",
        f"- 重复 trace_id 额外行数: {duplicate_extra_rows}",
        f"- language 非法值: {counter_text(invalid_languages)}",
        f"- 必备 extended_info 字段缺失: {counter_text(missing_extended)}",
        f"- 必备 extended_info 字段 null: {counter_text(null_extended)}",
        f"- safe 中 Attack Domain/Attack Method 非空: {safe_with_attack}",
        f"- unsafe 中 Attack Domain/Attack Method 至少一个非空: {unsafe_with_attack}",
        f"- unsafe 中 Attack Domain/Attack Method 同时为空: {unsafe_without_attack}",
        f"- 38类覆盖: {covered_risks}/{len(risk_types)}",
        f"- 缺失风险类别: {', '.join(f'`{risk}`' for risk in missing_risks) if missing_risks else '无'}",
        "",
        "## 一问多答",
        "",
        f"- 唯一 prompt 数（SHA-256 分组）: {unique_prompts}",
        f"- 回答数 >1 的 prompt 组数: {groups_over_1}",
        f"- 回答数 >10 的 prompt 组数: {groups_over_10}",
        f"- 单个 prompt 最大回答数: {max_answers}",
        f"- 超过 10 的超额回答数: {excess_over_10}",
        "",
        markdown_table(answer_groups, unique_prompts),
    ]
    if invalid_prompts:
        safe_metric_index = next(
            index for index, line in enumerate(report) if line.startswith("- safe 中")
        )
        report.insert(safe_metric_index, f"- prompt 非字符串: {invalid_prompts}")

    sections = (
        ("Label", simple["label"], rows),
        ("Language", simple["language"], rows),
        ("Case Type", extended["case_type"], rows),
        ("Risk Level", extended["risk_level"], rows),
        ("Attack Domain", extended["attack_domain"], rows),
        ("Attack Method", extended["attack_method"], rows),
        ("Attack Scenario", extended["attack_scenario"], rows),
        ("Is Attack", extended["is_attack"], rows),
        ("Quality Score", annotation["quality_score"], rows),
        ("Source", simple["source"], rows),
        ("Split", simple["split"], rows),
        ("Scene", simple["scene"], rows),
        ("Annotation Method", annotation["method"], rows),
        ("Label x Language", label_language, rows),
        ("Label x Case Type", label_case_type, rows),
        ("Label x Risk Level", label_risk_level, rows),
        ("Label x Split", label_split, rows),
        ("Unsafe x Attack Domain", label_attack_domain, rows),
        ("Unsafe x Risk Level x Attack Domain", unsafe_risk_domain, sum(unsafe_risk_domain.values())),
    )
    for title, counter, denominator in sections:
        report.extend(["", f"## {title}", "", markdown_table(counter, denominator)])

    report.extend(["", "## 38类覆盖", ""])
    report.extend([
        "| 风险类别 | Attack Domain | Attack Method | 主分类 | 是否存在 |",
        "|---|---:|---:|---:|---|",
    ])
    for risk in risk_types:
        report.append(
            f"| `{risk}` | {attack_domain_counts.get(risk, 0)} | "
            f"{attack_method_counts.get(risk, 0)} | {primary_risk_counts.get(risk, 0)} | "
            f"{'是' if primary_risk_counts.get(risk, 0) > 0 else '否'} |"
        )

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text("\n".join(report) + "\n", encoding="utf-8")
    print(
        f"report={output} rows={rows} unique_trace_ids={len(trace_ids)} "
        f"unique_prompts={unique_prompts} max_answers={max_answers}"
    )


if __name__ == "__main__":
    main()
