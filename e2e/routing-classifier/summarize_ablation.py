"""Join frozen Laya experiments with the actual Go rule classifier results."""
import argparse
import json
from pathlib import Path

from prompt_ablation import metrics


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", type=Path, required=True)
    parser.add_argument("--export-rules-input", action="store_true")
    args = parser.parse_args()
    frozen = json.loads((args.run / "frozen.json").read_text(encoding="utf-8"))
    cases = {c["id"]: c for c in frozen["cases"]}
    if args.export_rules_input:
        with (args.run / "rules-input.json").open("x", encoding="utf-8") as output:
            json.dump(frozen["cases"], output, ensure_ascii=False, indent=2)
        return
    rules = json.loads((args.run / "rules.json").read_text(encoding="utf-8"))
    if len(rules) != len(cases) or {r["id"] for r in rules} != set(cases):
        raise ValueError("incomplete or duplicate rule results")
    result = json.loads((args.run / "results.json").read_text(encoding="utf-8"))
    rule_rows = [dict(cases[r["id"]], tier=r["tier"], ms=r["latency_ms"]) for r in rules]
    summary = {"rules_v4": {g: metrics([r for r in rule_rows if g == "all" or r["group"] == g])
                             for g in ["all", "development", "new_synthetic", "reported_case"]}, **result["summary"]}
    baseline = {r["id"]: r for r in result["rows"] if r["variant"] == "A_v2_baseline"}
    paired = {}
    for name in result["summary"]:
        rows = [r for r in result["rows"] if r["variant"] == name]
        if len(rows) != len(cases) or {r["id"] for r in rows} != set(cases):
            raise ValueError("incomplete Laya results")
        paired[name] = {
            "fixed_vs_v2": [r["id"] for r in rows if r.get("tier") == r["expected_tier"] and baseline[r["id"]].get("tier") != r["expected_tier"]],
            "regressed_vs_v2": [r["id"] for r in rows if r.get("tier") != r["expected_tier"] and baseline[r["id"]].get("tier") == r["expected_tier"]],
        }
    lines = ["# Laya 提示词与实际 v4 规则对照", "", "参考标签为推理前编写的合成标注，未经独立人工复核；新题也不是独立盲测。", "",
             "|方案|历史题|新题|排序题|全部|低估/高估/失败|", "|---|---|---|---|---|---|"]
    for name, groups in summary.items():
        scores = [f"{groups[g]['correct']}/{groups[g]['total']}" for g in ["development", "new_synthetic", "reported_case", "all"]]
        total = groups["all"]
        lines.append(f"|{name}|" + "|".join(scores) + f"|{total['under']}/{total['over']}/{total['failures']}|")
    lines += ["", "## 相对 v2 的修正与退步", "", "|方案|修正题数|退步题数|", "|---|---|---|"]
    for name, comparison in paired.items():
        lines.append(f"|{name}|{len(comparison['fixed_vs_v2'])}|{len(comparison['regressed_vs_v2'])}|")
    lines += ["", "运行未更改默认提示词、路由开关或 K8s。历史题已用于规则开发，不能把规则历史题成绩当作泛化能力。"]
    with (args.run / "comparison-with-rules.md").open("x", encoding="utf-8") as output:
        output.write("\n".join(lines) + "\n")
    with (args.run / "comparison-with-rules.json").open("x", encoding="utf-8") as output:
        json.dump({"summary": summary, "paired": paired}, output, ensure_ascii=False, indent=2)
    print("\n".join(lines))


if __name__ == "__main__":
    main()
