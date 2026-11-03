"""Offline synthetic routing evaluation. Does not call the application gateway."""
import argparse
import hashlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import statistics
import time


TIER_LABELS = {"simple": 1, "medium": 2, "complex": 3, "reasoning": 4}
# Freeze the rubric before evaluating. Expected labels/notes are NEVER model input.
QUESTIONS = {
    "tier": {
        "type": "choice",
        "instructions": "判断当前用户任务需要的能力档位。历史仅用于理解续问，新话题不要继承旧难度。按实际要求判断，不要只看出现的术语。",
        "criteria": {
            "simple": "简单问候、事实、定义、算术、翻译、摘要或提取已有信息。",
            "medium": "常规代码编写与调试、一般写作、文案和比较分析。",
            "complex": "系统架构、复杂故障诊断、多环节业务方案或多个约束的权衡。",
            "reasoning": "要求严格数学证明、公式推导、算法正确性或形式化论证。",
        },
    }
}


def percentile(values, fraction):
    values = sorted(values)
    return values[max(0, math.ceil(len(values) * fraction) - 1)] if values else None


def summarize(rows, key):
    correct = under = over = failures = 0
    confusion = [[0] * 4 for _ in range(4)]
    latencies = []
    for row in rows:
        prediction = row[key]
        tier = prediction.get("tier")
        expected = row["expected_tier"]
        if tier not in (1, 2, 3, 4):
            failures += 1
            continue
        correct += tier == expected
        under += tier < expected
        over += tier > expected
        confusion[expected - 1][tier - 1] += 1
        latencies.append(prediction["latency_ms"])
    return {"total": len(rows), "correct": correct, "accuracy": correct / len(rows),
            "under": under, "over": over, "failures": failures,
            "confusion_expected_rows_predicted_columns": confusion,
            "p50_ms": statistics.median(latencies) if latencies else None,
            "p95_ms": percentile(latencies, .95)}


def render_report(data):
    meta, summary = data["metadata"], data["summary"]
    lines = ["# Laya 与 ClawManager 规则：中文离线对照", "",
             "这是预先标注的 40 条合成题、小样本开发评测，不是盲测、独立人工标注或生产准确率。",
             "core 检查已知常见场景，challenge 检查口语换说法、混合语言和续问；参考档位遵循项目的四档政策。",
             "没有训练、调参或在本轮看到结果后改写题目/提示。没有真实用户会话或正式路由流量。", "",
             f"- Laya 包：{meta['laya_version']}；PyTorch：{meta['torch_version']}。",
             f"- 模型：`{meta['model_repo']}`，revision：`{meta['model_revision']}`。",
             f"- 设备：CPU，{meta['threads']} 线程；操作系统：{meta['platform']}。",
             f"- 模型加载耗时：{meta['load_seconds']:.2f}s（不计下载、不计入逐题延迟）。",
             f"- 配置：max_len={meta['max_len']}，head_max_len={meta['head_max_len']}；预热 3 次，不做批处理。",
             f"- 题集 SHA256：`{meta['cases_sha256']}`。", "",
             "规则延迟为每题 5 批、每批至少 20ms 的均摊单次耗时中位数；Laya 为每题一次的本地同步调用耗时。均不含网络和进程启动，不能作为服务 SLA。", "",
             "## 汇总", "", "|方案|正确/总数|一致率|低估|高估|失败|P50 ms|P95 ms|",
             "|---|---:|---:|---:|---:|---:|---:|---:|"]
    for name in ("rule", "laya"):
        s = summary[name]
        p50 = "N/A" if s["p50_ms"] is None else f"{s['p50_ms']:.3f}"
        p95 = "N/A" if s["p95_ms"] is None else f"{s['p95_ms']:.3f}"
        lines.append(f"|{name}|{s['correct']}/{s['total']}|{s['accuracy']:.1%}|{s['under']}|{s['over']}|{s['failures']}|{p50}|{p95}|")
    lines += ["", "## 分组", "", "|题组|规则一致率|Laya 一致率|", "|---|---:|---:|"]
    for suite, scores in data["suites"].items():
        lines.append(f"|{suite}|{scores['rule']['accuracy']:.1%}|{scores['laya']['accuracy']:.1%}|")
    lines += ["", "## 逐题结果", "", "历史见 JSON；此表只展示最新提问。概率为模型返回值，不保证经过本任务校准。", "",
              "|ID|题组|最新提问|参考档|规则|Laya|最大选项概率|", "|---|---|---|---:|---:|---:|---:|"]
    for row in data["rows"]:
        prompt = row["turns"][-1]["text"].replace("|", "\\|").replace("\n", " ")
        p = row["laya"].get("top_probability")
        confidence = "N/A" if p is None else f"{p:.3f}"
        lines.append(f"|{row['id']}|{row['suite']}|{prompt}|{row['expected_tier']}|{row['rule']['tier']}|{row['laya'].get('tier', 'ERR')}|{confidence}|")
    lines += ["", "## 解读边界", "",
              "- 这测的是固定任务政策下的能力档位，不是模型回答质量，也不是对所有任务的客观难度评分。",
              "- Laya 直接输出参与统计，不用规则补齐失败，不设置事后置信度阈值，不掩盖误分类。",
              "- 两者使用相同用户文本；Laya 额外接收固定分档标准，规则的标准编码在程序中。参考答案与备注未发送给 Laya。",
              "- 需要独立人工复核标注、更大题集和真实环境压测，才能决定是否启用或微调。", ""]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cases", type=Path, required=True)
    parser.add_argument("--rules", type=Path, required=True)
    parser.add_argument("--cache", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--download", action="store_true", help="explicitly permit public model download")
    parser.add_argument("--revision", default="main")
    parser.add_argument("--threads", type=int, default=4)
    parser.add_argument("--reuse-laya-results", type=Path, help="reuse archived inference; remeasure only the rule baseline")
    args = parser.parse_args()
    if args.threads < 1:
        parser.error("threads must be positive")
    # Fail rather than overwrite previous measurement artifacts.
    args.output.mkdir(parents=True, exist_ok=False)
    corpus_raw = args.cases.read_bytes()
    cases = json.loads(corpus_raw)
    rule_rows = json.loads(args.rules.read_text(encoding="utf-8"))
    rules = {r["id"]: r for r in rule_rows}
    if len(rules) != len(rule_rows) or len({c['id'] for c in cases}) != len(cases) or set(rules) != {c['id'] for c in cases}:
        raise ValueError("case/rule IDs do not match")
    if args.reuse_laya_results:
        previous = json.loads(args.reuse_laya_results.read_text(encoding="utf-8"))
        if previous["metadata"]["cases_sha256"] != hashlib.sha256(corpus_raw).hexdigest() or previous["metadata"]["questions"] != QUESTIONS:
            raise ValueError("cannot reuse results for a changed corpus/rubric")
        if len(previous["rows"]) != len(cases) or {r['id'] for r in previous['rows']} != set(rules):
            raise ValueError("incomplete archived inference")
        for row in previous["rows"]:
            if rules[row['id']]['tier'] != row['rule']['tier']:
                raise ValueError("rule classification changed, not just timing")
            row["rule"] = rules[row["id"]]
        previous["metadata"]["laya_reused_from"] = str(args.reuse_laya_results)
        previous["metadata"]["rule_latency_method"] = "median of five >=20ms batches, amortized per call"
        write_results(args.output, previous["metadata"], previous["rows"])
        return
    # Use only an isolated cache, never touch a shared HF model snapshot.
    os.environ["HF_HOME"] = str(args.cache.resolve())
    os.environ["HF_HUB_DISABLE_TELEMETRY"] = "1"
    os.environ["TOKENIZERS_PARALLELISM"] = "false"
    if not args.download:
        os.environ["HF_HUB_OFFLINE"] = "1"
    import torch
    from huggingface_hub import HfApi, snapshot_download
    import laya
    torch.set_num_threads(args.threads)
    torch.set_num_interop_threads(1)
    torch.manual_seed(0)
    repo = "convaiinnovations/laya-multilingual"
    revision = HfApi().model_info(repo, revision=args.revision).sha if args.download else args.revision
    print(f"Loading {repo}@{revision}", flush=True)
    model_path = snapshot_download(repo, revision=revision, cache_dir=str(args.cache.resolve()),
                                   local_files_only=not args.download,
                                   allow_patterns=["rl_agent_config.json", "model.safetensors", "tokenizer/*", "encoder/*"])
    load_started = time.perf_counter()
    agent = laya.Agent(model_path, device="cpu")
    load_seconds = time.perf_counter() - load_started
    max_len, head_max_len = 1024, 512
    def state(c):
        return {"prior_user_turns": [t["text"] for t in c["turns"][:-1]],
                "current_user_task": c["turns"][-1]["text"]}
    # Check the FULL schema and text fit, including option labels and schema
    # overhead. Do not accept silent truncation as a model error in this test.
    from laya.common import render_options, serialize_state
    internal = agent._to_internal(QUESTIONS["tier"])
    option_text = render_options(internal)
    question_text = internal["ins"] if "ins" in internal else QUESTIONS["tier"]["instructions"]
    option_token_counts = [len(agent.tok.encode(" " + text, add_special_tokens=False)) for text in option_text]
    if any(count > 48 for count in option_token_counts):
        raise ValueError(f"option exceeds Laya's 48-token per-option cap: {option_token_counts}")
    schema_tokens = len(agent.tok.encode(str(question_text) + str(option_text), add_special_tokens=True))
    if schema_tokens > head_max_len - 32:
        raise ValueError(f"schema too long: {schema_tokens} tokens")
    for c in cases:
        state_tokens = len(agent.tok.encode(serialize_state(state(c)), add_special_tokens=True))
        if state_tokens > max_len - head_max_len - 32:
            raise ValueError(f"case {c['id']} too long: {state_tokens} tokens")
    for _ in range(3):
        agent.predict(state(cases[0]), QUESTIONS, max_len=max_len, head_max_len=head_max_len)
    meta = {"laya_version": importlib.metadata.version("laya"), "torch_version": torch.__version__,
            "model_repo": repo, "model_revision": revision, "model_path": str(model_path),
            "platform": platform.platform(), "processor": platform.processor(), "threads": args.threads,
            "load_seconds": load_seconds, "cases_sha256": hashlib.sha256(corpus_raw).hexdigest(),
            "max_len": max_len, "head_max_len": head_max_len, "schema_tokens_estimate": schema_tokens,
            "option_token_counts": option_token_counts,
            "questions": QUESTIONS, "synthetic": True, "reference_labels": "assistant-authored before inference; unreviewed"}
    rows = []
    with (args.output / "partial.jsonl").open("x", encoding="utf-8") as partial:
        for c in cases:
            start = time.perf_counter()
            try:
                response = agent.predict(state(c), QUESTIONS, max_len=max_len, head_max_len=head_max_len)
                elapsed_ms = (time.perf_counter() - start) * 1000
                answer = response["answers"]["tier"]
                tier = TIER_LABELS[answer["choice"]]
                prediction = {"tier": tier, "latency_ms": elapsed_ms,
                              "top_probability": max(answer["probabilities"].values()), "raw_response": response}
            except Exception as exc:
                prediction = {"tier": None, "latency_ms": (time.perf_counter() - start) * 1000,
                              "error": f"{type(exc).__name__}: {exc}"}
            row = {**c, "rule": rules[c["id"]], "laya": prediction}
            rows.append(row)
            partial.write(json.dumps(row, ensure_ascii=False) + "\n")
            partial.flush()
            print(f"{c['id']}: expected={c['expected_tier']} rule={rules[c['id']]['tier']} laya={prediction['tier']} {prediction['latency_ms']:.1f}ms", flush=True)
    write_results(args.output, meta, rows)


def write_results(output, meta, rows):
    data = {"metadata": meta, "rows": rows,
            "summary": {name: summarize(rows, name) for name in ("rule", "laya")},
            "suites": {suite: {name: summarize([r for r in rows if r['suite'] == suite], name) for name in ("rule", "laya")} for suite in sorted({r['suite'] for r in rows})}}
    (output / "results.json").write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    (output / "report.md").write_text(render_report(data), encoding="utf-8")
    print(json.dumps(data["summary"], ensure_ascii=False, indent=2), flush=True)


if __name__ == "__main__":
    main()
