"""Frozen, offline Laya rubric ablation. Synthetic text only; no HTTP/keys.

Run with the SAME deployed Laya image and embedded multilingual checkpoint.
Writes new evaluation artifacts, never overwrites an existing output directory.
Reference labels and case IDs are never included in inference input.
"""
import argparse
import copy
import hashlib
import importlib.metadata
import importlib.util
import json
import os
from pathlib import Path
import statistics
import time

TIERS = {"simple": 1, "medium": 2, "complex": 3, "reasoning": 4}
SHORT = "判断最后一个用户任务需要哪类能力。历史仅用于理解续问。按实际要完成的工作选择一项。"
CONCRETE = {
    "simple": "问候、查常识、简单计算、翻译一句话、提取已有信息。",
    "medium": "写小函数或网页、修局部代码、写邮件文案、解释比较。",
    "complex": "设计跨服务系统、排查联动故障、权衡多项约束的整体方案。",
    "reasoning": "严格证明定理或算法正确性，完成非平凡数学推导。",
}


def load_baseline(path):
    spec = importlib.util.spec_from_file_location("laya_baseline_client", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def variants(baseline):
    result = []
    for name, short, concrete, plain in [
        ("A_v2_baseline", False, False, False),
        ("B_short_only", True, False, False),
        ("C_concrete_only", False, True, False),
        ("D_plain_only", False, False, True),
        ("E_combined", True, True, True),
    ]:
        question = copy.deepcopy(baseline)
        if short:
            question["instructions"] = SHORT
        if concrete:
            question["criteria"] = dict(CONCRETE)
        result.append({"name": name, "question": question, "format": "plain" if plain else "json"})
    return result


def state_for(turns, form):
    # Match Go json.Marshal including HTML escaping, with no reference metadata.
    if form == "json":
        value = json.dumps([{"text": t["text"], "has_non_text": False, "truncated": False} for t in turns], ensure_ascii=False, separators=(",", ":"))
        for char, escape in [("<", "\\u003c"), (">", "\\u003e"), ("&", "\\u0026"), ("\u2028", "\\u2028"), ("\u2029", "\\u2029")]:
            value = value.replace(char, escape)
        return value
    if len(turns) == 1:
        return turns[0]["text"]
    return "\n".join(f"{'当前任务' if i == len(turns)-1 else '历史用户任务'}：{t['text']}" for i, t in enumerate(turns))


def metrics(rows):
    good = [r for r in rows if r.get("tier") in TIERS.values()]
    correct = sum(r["tier"] == r["expected_tier"] for r in good)
    confusion = [[0] * 4 for _ in range(4)]
    for r in good:
        confusion[r["expected_tier"] - 1][r["tier"] - 1] += 1
    latencies = sorted(r["ms"] for r in rows)
    return {"total": len(rows), "correct": correct, "agreement": correct / len(rows) if rows else 0,
            "failures": len(rows) - len(good), "under": sum(r["tier"] < r["expected_tier"] for r in good),
            "over": sum(r["tier"] > r["expected_tier"] for r in good), "confusion": confusion,
            "p50_ms": statistics.median(latencies) if latencies else 0,
            "per_tier": {str(t): {"total": sum(r["expected_tier"] == t for r in rows), "correct": sum(r.get("tier") == t and r["expected_tier"] == t for r in rows)} for t in range(1, 5)}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-client", type=Path, required=True)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--threads", type=int, default=2)
    args = parser.parse_args()
    if not 1 <= args.threads <= 8:
        parser.error("threads must be 1..8")
    os.environ.update(HF_HUB_OFFLINE="1", TRANSFORMERS_OFFLINE="1", TOKENIZERS_PARALLELISM="false")
    baseline = load_baseline(args.baseline_client)
    configs = variants(baseline.QUESTIONS["tier"])
    root = Path(__file__).resolve().parent
    cases, hashes = [], {}
    for file, group in [("cases.zh.json", "development"), ("prompt-holdout.zh.json", "new_synthetic")]:
        raw = (root / file).read_bytes()
        hashes[file] = hashlib.sha256(raw).hexdigest()
        cases.extend(dict(c, group=group) for c in json.loads(raw))
    cases.append({"id": "reported_sort", "group": "reported_case", "expected_tier": 2,
                  "turns": [{"text": "帮我写一个 Python 排序函数"}]})
    if len({c["id"] for c in cases}) != len(cases):
        raise ValueError("duplicate case IDs")
    # Freeze all prompts and references before loading weights or inferring.
    args.output.mkdir(parents=True, exist_ok=False)
    frozen = {"configs": configs, "cases": cases, "hashes": hashes, "baseline_version": baseline.TIER_PROMPT_VERSION}
    (args.output / "frozen.json").write_text(json.dumps(frozen, ensure_ascii=False, indent=2), encoding="utf-8")
    import torch
    import laya
    from laya.common import render_options
    torch.set_num_threads(args.threads)
    torch.set_num_interop_threads(1)
    torch.manual_seed(0)
    started = time.perf_counter()
    agent = laya.Agent(str(args.model), device="cpu")
    metadata = {"laya_version": importlib.metadata.version("laya"), "torch_version": torch.__version__,
                "load_seconds": time.perf_counter() - started, "threads": args.threads, "device": "cpu",
                "max_len": 2048, "head_max_len": 512, "baseline_version": baseline.TIER_PROMPT_VERSION,
                "labels": "assistant-authored synthetic references, not independently reviewed",
                "image": "10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1", "hashes": hashes}
    lengths = {}
    for cfg in configs:
        q = agent._to_internal(cfg["question"])
        head = len(agent.tok.encode("choice question: " + q["ins"], add_special_tokens=False))
        opts = [len(agent.tok.encode(" " + opt, add_special_tokens=False)) for opt in render_options(q)]
        total = head + sum(n + 1 for n in opts)
        if max(opts) > 48 or total > 512:
            raise ValueError(f"prompt truncation: {cfg['name']} {head} {opts}")
        lengths[cfg["name"]] = {"instructions": head, "options": opts, "head_total": total}
        for c in cases:
            state = state_for(c["turns"], cfg["format"])
            if len(state_for(c["turns"], "json").encode("utf-8")) > 1200:
                raise ValueError("case exceeds Gateway input budget: " + c["id"])
            if total + len(agent.tok.encode(state, add_special_tokens=False)) + 4 > 2048:
                raise ValueError("state truncation: " + c["id"])
    metadata["prompt_tokens"] = lengths
    # Equal warm-up per variant; no retries or response correction.
    for cfg in configs:
        agent.predict(state_for(cases[0]["turns"], cfg["format"]), {"tier": cfg["question"]}, max_len=2048, head_max_len=512)
    rows = []
    with (args.output / "partial.jsonl").open("x", encoding="utf-8") as output:
        for index, c in enumerate(cases):
            # Rotate order to reduce systematic warm-up/thermal timing bias.
            order = configs[index % len(configs):] + configs[:index % len(configs)]
            for cfg in order:
                start = time.perf_counter()
                row = {"id": c["id"], "group": c["group"], "expected_tier": c["expected_tier"], "variant": cfg["name"]}
                try:
                    response = agent.predict(state_for(c["turns"], cfg["format"]), {"tier": cfg["question"]}, max_len=2048, head_max_len=512)
                    answer = response["answers"]["tier"]
                    probs = answer["probabilities"]
                    ranking = sorted(probs.values(), reverse=True)
                    row.update(tier=TIERS[answer["choice"]], probabilities=probs, entropy_confidence=answer.get("confidence"),
                               top_probability=ranking[0], probability_margin=ranking[0]-ranking[1])
                except Exception as exc:
                    row.update(tier=None, error=type(exc).__name__)
                row["ms"] = (time.perf_counter() - start) * 1000
                rows.append(row)
                output.write(json.dumps(row, ensure_ascii=False) + "\n")
                output.flush()
            print(f"{index + 1}/{len(cases)} {c['id']} " + " ".join(f"{r['variant'][0]}={r['tier']}" for r in rows[-5:]), flush=True)
    summary = {cfg["name"]: {group: metrics([r for r in rows if r["variant"] == cfg["name"] and (group == "all" or r["group"] == group)])
                                  for group in ["all", "development", "new_synthetic", "reported_case"]} for cfg in configs}
    result = {"metadata": metadata, "summary": summary, "rows": rows}
    (args.output / "results.json").write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    lines = ["# Laya 提示词消融实验", "", "合成参考标签一致率，不是生产准确率。新题未独立人工复核；未启用或修改正式路由。", "",
             "|版本|全部一致|历史开发题|新合成题|已报告排序题|低估/高估/失败|P50 ms|", "|---|---|---|---|---|---|---|"]
    for name, groups in summary.items():
        values = [f"{groups[g]['correct']}/{groups[g]['total']}" for g in ["all", "development", "new_synthetic", "reported_case"]]
        all_ = groups["all"]
        lines.append(f"|{name}|" + "|".join(values) + f"|{all_['under']}/{all_['over']}/{all_['failures']}|{all_['p50_ms']:.1f}|")
    lines += ["", "完整提示词、参考题和哈希见 frozen.json；逐题概率、耗时及分档混淆矩阵见 results.json。", "本机离线推理耗时不含 HTTP、服务器排队，不能当作远端延迟。"]
    (args.output / "report.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
