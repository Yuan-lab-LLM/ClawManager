#!/usr/bin/env python3
"""Summarize frozen slot-reset evidence and render an offline HTML report."""
import argparse
import base64
from collections import Counter
from datetime import datetime
import html
import json
import math
from pathlib import Path
import re
from urllib.parse import quote
import zipfile

from run_slot_reset_evidence import IDENTIFIER, SCOPES, digest, read_source_manifest

REQUIREMENTS = (
    "8 个 slot 中 1 个异常或未知，其余 7 个已满。创建触发新 Pod 并成功，旧异常保持隔离。",
    "一个 Pod 全部异常或未知且无健康源 Pod。批准配置与控制器一致时可扩容，真实漂移时报告具体错误。",
    "异常 slot 与已核实空闲 slot 同 Pod 时使用空闲 slot。Pod 整体未知时不向该 Pod 分配。",
    "批量创建、并发调度、在途扩容、长期 Pending 与 CPU/内存不足时，避免抢异常 slot、无限扩容或覆盖人工副本数。",
    "手动重置终止目标网站进程与 worker，保留工作区，邻居进程和实际访问持续正常。",
    "旧 slot 残留实例 A 时清理旧执行，其他 Pod/slot 上 A 的 binding、代次、凭据、页面与健康保持有效，含同代次情况。",
    "覆盖无 SQL binding、无实例记录、未知属主及 worker 不响应的物理重置。缺少退出凭据时保留隔离。",
    "预检后属主或容器变化、解绑时 binding 变化。条件写保护新 binding 与新执行。",
    "双击、重复回调、CM/helper 中断、超时、自动替换与长退避时可续跑，保护新容器。",
    "迟到心跳与发布无法复活旧源。同类型、跨类型创建、原生升级和数据安全边界保持正确。",
    "分别验证正常停止、休眠、删除及其失败阶段和界面状态。休眠证明失败时明确报告。",
    "验证管理员权限、预检影响、任务进度、错误与重试，以及真实浏览器桌面和窄屏交互。",
    "期望运行且无其他执行的实例在确认旧源退出后持久待调度、恢复或扩容，原 slot 隔离与退避不阻塞恢复。",
    "验证解绑与恢复意图的事务原子性、提交后 CM 重启和重复恢复回调。恢复代次只前进一次且只有一个执行。",
    "沿用其他 slot 的启动中分配。重置或待调度期间的停止、休眠及删除阻止实例复活。",
)
RUNTIMES = {"openclaw": "OpenClaw", "hermes": "Hermes", "opencode": "OpenCode", "dsh": "DeepSeek Harness"}
SCOPES_ZH = {
    "backend": "后端测试", "frontend": "前端测试与构建", "sql-redis": "SQL / Redis 集成",
    "cluster": "实际隔离集群", "browser-fixture": "浏览器夹具（模拟 API）",
    "browser-live": "实际集群浏览器", "native-upgrade": "CM 原生 Runtime 升级",
    "native-protection": "候选镜像原生保护检查", "report": "报告校验",
}
STATUSES = ("PASS", "FAIL", "SKIP", "NOT RUN")
TERMINAL = {"pass": "PASS", "fail": "FAIL", "skip": "SKIP", "build-fail": "FAIL"}
SCOPE_REQUIRED = {5: {"cluster", "browser-live"}, 12: {"browser-live"}}
PREVIEW_CHARS = 16000
PRIVATE_VALUES = (
    (re.compile(r"(?i)\b((?:https?|mysql|postgres(?:ql)?|redis)://)[^:/@\\\s\x22\x27]+:[^/@\\\s\x22\x27]+@"), r"\g<1>[redacted]@"),
    (re.compile(r"\b[\w.-]+:[^@\\\s\x22\x27]+@tcp\("), "[redacted]@tcp("),
    (re.compile(r"\beyJ[A-Za-z0-9_-]{12,}\.eyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{15,}"), "[redacted-jwt]"),
    (re.compile(r"(?i)\b(Bearer )[A-Za-z0-9_.~+/=-]{12,}"), r"\g<1>[redacted]"),
)


def load_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def redact(text):
    for pattern, replacement in PRIVATE_VALUES:
        text = pattern.sub(replacement, text)
    return text


def timestamp(value):
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("timestamps require a timezone")
    return parsed


def combine(statuses):
    for status in ("FAIL", "NOT RUN", "SKIP"):
        if status in statuses:
            return status
    return "PASS" if statuses else "NOT RUN"


def read_go_output(raw):
    cases, packages, started, current = [], {}, set(), {}
    malformed = 0
    for line in raw.splitlines():
        if not line.strip():
            continue
        try:
            event = json.loads(line)
            action = event["Action"]
            package = event.get("Package") or event.get("ImportPath")
            test = event.get("Test")
            if not isinstance(action, str) or not isinstance(package, str) or (test is not None and not isinstance(test, str)):
                raise ValueError("invalid Go event")
        except (KeyError, ValueError, TypeError, AttributeError):
            malformed += 1
            continue
        if test:
            key = (package, test)
            if action == "run" or key not in current:
                row = {"package": package, "test": test, "status": "NOT RUN", "elapsed_seconds": None, "output_preview": ""}
                current[key] = row
                cases.append(row)
            row = current[key]
            if action in TERMINAL:
                row.update(status=TERMINAL[action], elapsed_seconds=event.get("Elapsed"))
            if action == "output" and isinstance(event.get("Output"), str):
                row["output_preview"] = (row["output_preview"] + event["Output"])[-PREVIEW_CHARS:]
        elif action == "start":
            started.add(package)
        elif action in TERMINAL:
            status = TERMINAL[action]
            if packages.get(package, {}).get("status") != "FAIL":
                packages[package] = {"package": package, "status": status, "elapsed_seconds": event.get("Elapsed")}
    for package in started - packages.keys():
        packages[package] = {"package": package, "status": "NOT RUN", "elapsed_seconds": None}
    return cases, sorted(packages.values(), key=lambda row: row["package"]), malformed


def read_suite(record_path, label, frozen, fingerprint):
    row = load_json(record_path)
    suite = row["suite_id"]
    if not IDENTIFIER.fullmatch(suite) or row["scope"] not in SCOPES or row["kind"] not in ("go-json", "command"):
        raise ValueError(f"invalid suite metadata: {suite}")
    if row["phase"] != "final" or row["run_label"] != label or timestamp(row["started_at"]) < frozen:
        raise ValueError(f"not a final recording for this freeze and run label: {suite}")
    if row["source_manifest_sha256"] != fingerprint:
        raise ValueError(f"suite source differs from freeze: {suite}")
    evidence = record_path.parent / row["evidence"]
    contents = evidence.read_bytes()
    raw = contents.decode("utf-8", errors="replace")
    row.update(status="NOT RUN", packages=[], cases=[], log=f"raw/{suite}.log",
               record=f"records/{suite}.json", output_preview=raw[-PREVIEW_CHARS:])
    if row["exit_code"] is None:
        row["reason"] = "命令没有完成记录"
        return row, evidence, raw
    if type(row["exit_code"]) is not int:
        raise ValueError(f"invalid exit code: {suite}")
    elapsed = row["elapsed_seconds"]
    wall = (timestamp(row["finished_at"]) - timestamp(row["started_at"])).total_seconds()
    if not math.isfinite(elapsed) or elapsed < 0 or wall < 0 or abs(wall - elapsed) > max(5, elapsed * .02):
        raise ValueError(f"inconsistent suite timing: {suite}")
    if digest(contents) != row.get("evidence_sha256") or len(contents) != row.get("evidence_bytes"):
        raise ValueError(f"evidence differs from recording: {suite}")
    if row.get("source_verified_after") is not True:
        raise ValueError(f"source verification did not finish successfully: {suite}")

    status = "FAIL" if row["exit_code"] else "PASS"
    if row["kind"] == "go-json":
        cases, packages, malformed = read_go_output(raw)
        row.update(cases=cases, packages=packages, malformed_lines=malformed)
        states = {case["status"] for case in cases}
        pkg_states = {pkg["status"] for pkg in packages}
        if "FAIL" in states | pkg_states:
            status = "FAIL"
        elif not row["exit_code"]:
            complete = packages and "NOT RUN" not in states | pkg_states and not malformed
            if not complete or not states:
                status = "NOT RUN"
                row["reason"] = "Go 记录不完整或未执行测试"
            else:
                status = "PASS" if "PASS" in states else "SKIP"
    elif not row["exit_code"] and not raw.strip():
        status = "NOT RUN"
        row["reason"] = "命令未记录可查阅的输出"
    row["status"] = status
    return row, evidence, raw


def resolve_evidence(refs, suites, raw_logs):
    results = []
    for ref in refs:
        suite = suites.get(ref["suite"])
        status = suite["status"] if suite else "NOT RUN"
        reason = ""
        if suite and ref.get("test"):
            matching = [case for case in suite["cases"]
                        if case["test"] == ref["test"] and (not ref.get("package") or case["package"] == ref["package"])]
            status = combine([case["status"] for case in matching])
            if suite["status"] in ("FAIL", "NOT RUN"):
                status = combine([status, suite["status"]])
            if not matching:
                reason = "缺少对应测试事件"
        marker = ref.get("contains")
        if marker and marker not in raw_logs.get(ref["suite"], ""):
            status = combine([status, "NOT RUN"])
            reason = "原始证据中缺少该文本"
        results.append(dict(ref, status=status, scope=suite["scope"] if suite else None,
                            phase=suite["phase"] if suite else None,
                            linked=suite is not None, reason=reason))
    return results


def acceptance_rows(entries, suites, raw_logs, sample, artifacts):
    if set(entries) - {str(number) for number in range(1, len(REQUIREMENTS) + 1)}:
        raise ValueError("unknown acceptance requirement number")
    for number, criterion in enumerate(REQUIREMENTS, start=1):
        checks = []
        for entry in entries.get(str(number), []):
            refs = resolve_evidence(entry["evidence"], suites, raw_logs)
            status = combine([ref["status"] for ref in refs])
            if not entry["expected"].strip() or not entry["actual"].strip():
                status = combine([status, "NOT RUN"])
            linked_artifacts = entry.get("artifacts", [])
            if set(linked_artifacts) - artifacts.keys():
                raise ValueError(f"unknown artifact in acceptance {number}")
            checks.append(dict(entry, evidence=refs, status=status))
        status = combine([check["status"] for check in checks])
        scopes = {ref["scope"] for check in checks for ref in check["evidence"] if ref["status"] == "PASS"}
        required = SCOPE_REQUIRED.get(number, set())
        if status == "PASS" and required and not required & scopes and (number == 12 or not sample):
            status = "NOT RUN"
            reason = "缺少实际集群证据"
        else:
            reason = ""
        yield {"id": number, "criterion": criterion, "status": status, "checks": checks, "reason": reason}


def operational_row(entry, suites, raw_logs, scopes, refusal_scopes=()):
    entry = entry or {}
    refs = resolve_evidence(entry.get("evidence", []), suites, raw_logs)
    stated = entry.get("status", "NOT RUN")
    if stated not in (*STATUSES, "REFUSED", "BLOCKED"):
        raise ValueError(f"unknown operation status: {stated}")
    actual = [ref for ref in refs if ref["scope"] in scopes]
    status = stated
    if stated == "PASS":
        status = combine([ref["status"] for ref in refs]) if actual else "NOT RUN"
    elif stated in ("REFUSED", "BLOCKED"):
        verified = [ref for ref in refs if ref["scope"] in scopes or ref["scope"] in refusal_scopes]
        verified = [ref for ref in verified if ref.get("contains") and not ref["reason"]
                    and ref["status"] in ("PASS", "FAIL") and not ref.get("test")]
        if not verified or not entry.get("detail"):
            status = "NOT RUN"
    elif stated == "FAIL" and not any(ref["status"] == "FAIL" for ref in actual):
        status = "NOT RUN"
    elif stated == "SKIP" and not entry.get("detail"):
        status = "NOT RUN"
    return dict(entry, reported_status=stated, status=status, evidence=refs)


def read_supporting_run(entry, directory):
    suite_id = entry["suite_id"]
    if (not IDENTIFIER.fullmatch(suite_id) or entry["scope"] not in SCOPES_ZH
            or entry["status"] not in STATUSES or not entry["source_revision"].strip()
            or not entry["command"].strip()):
        raise ValueError(f"invalid supporting run: {suite_id}")
    finish = timestamp(entry["finished_at"])
    if entry.get("started_at") and timestamp(entry["started_at"]) > finish:
        raise ValueError(f"inconsistent supporting run timing: {suite_id}")
    path = directory / entry["evidence"]
    content = path.read_bytes()
    if not content or digest(content) != entry["evidence_sha256"]:
        raise ValueError(f"supporting evidence differs from recording: {suite_id}")
    raw = content.decode("utf-8", errors="replace")
    row = dict(entry, phase="supporting", cases=[], log=f"raw/{suite_id}.log", output_preview=raw[-PREVIEW_CHARS:])
    return row, path, raw


def build_report(manifest_path, basename):
    manifest = load_json(manifest_path)
    directory = manifest_path.parent
    source = manifest["source"]
    frozen = timestamp(manifest["code_frozen_at"])
    source_path = directory / source["manifest"]
    source_raw, entries = read_source_manifest(source_path)
    fingerprint = digest(source_raw)
    if source["manifest_sha256"] != fingerprint:
        raise ValueError("source manifest differs from freeze")
    label = manifest["run_label"]
    if not isinstance(label, str) or not label.strip():
        raise ValueError("run label must be nonempty")
    sample = manifest.get("sample") is True
    suites, logs = {}, {}
    files = {"source-manifest.json": source_path, "input-manifest.json": manifest_path}
    for filename in manifest["suites"]:
        record = directory / filename
        suite, evidence, raw = read_suite(record, label, frozen, fingerprint)
        suite_id = suite["suite_id"]
        if suite_id in suites:
            raise ValueError(f"duplicate final suite: {suite_id}")
        suites[suite_id] = suite
        logs[suite_id] = raw
        files[suite["log"]] = evidence
        files[suite["record"]] = record

    supporting = {}
    for item in manifest.get("supporting_runs", []):
        run, evidence, raw = read_supporting_run(item, directory)
        suite_id = run["suite_id"]
        if suite_id in suites or suite_id in supporting:
            raise ValueError(f"duplicate supporting run: {suite_id}")
        supporting[suite_id] = run
        logs[suite_id] = raw
        files[run["log"]] = evidence

    artifacts = {}
    for name, item in manifest.get("artifacts", {}).items():
        if not IDENTIFIER.fullmatch(name):
            raise ValueError(f"invalid artifact identifier: {name}")
        path = directory / item["path"]
        suffix = path.suffix if IDENTIFIER.fullmatch(path.suffix.removeprefix(".")) else ""
        link = f"artifacts/{name}{suffix}"
        content = path.read_bytes()
        if item.get("captured_at"):
            timestamp(item["captured_at"])
        artifacts[name] = dict(item, link=link, sha256=digest(content), bytes=len(content))
        files[link] = path

    evidence_runs = suites | supporting
    acceptance = list(acceptance_rows(manifest["acceptance"], evidence_runs, logs, sample, artifacts))
    required = {name: suites.get(name, {}).get("status", "NOT RUN") for name in manifest["required_suites"]}
    test_counts = Counter(case["status"] for suite in suites.values() for case in suite["cases"])
    suite_counts = Counter(suite["status"] for suite in suites.values())
    status = combine([row["status"] for row in acceptance] + list(required.values()))
    if status == "PASS" and not required:
        status = "NOT RUN"
    for key in ("native_upgrades", "runtime_validation"):
        extra = set(manifest.get(key, {})) - RUNTIMES.keys()
        if extra:
            raise ValueError(f"unknown runtime type in {key}: {', '.join(sorted(extra))}")
    native = {kind: operational_row(manifest.get("native_upgrades", {}).get(kind), evidence_runs, logs,
                                    {"native-upgrade"}, {"native-protection"})
              for kind in RUNTIMES}
    runtimes = {kind: operational_row(manifest.get("runtime_validation", {}).get(kind), evidence_runs, logs,
                                      {"cluster", "browser-live"})
                for kind in RUNTIMES}
    deployment = operational_row(manifest.get("deployment"), suites, logs, {"cluster", "browser-live"})
    return {
        "schema_version": 1, "run_label": label, "sample": sample, "code_frozen_at": manifest["code_frozen_at"],
        "source": dict(source, manifest="source-manifest.json", file_count=len(entries)),
        "environment": manifest["environment"], "acceptance_status": status, "accepted": status == "PASS" and not sample,
        "acceptance": acceptance, "acceptance_counts": dict(Counter(row["status"] for row in acceptance)),
        "test_counts": dict(test_counts), "suite_counts": dict(suite_counts), "required_suites": required,
        "suites": list(suites.values()), "supporting_runs": list(supporting.values()),
        "artifacts": artifacts, "native_upgrades": native,
        "runtime_validation": runtimes, "deployment": deployment, "limitations": manifest.get("limitations", []),
        "regressions": manifest.get("regressions", []), "bundle": basename + "-evidence.zip",
    }, files


def esc(value):
    return html.escape(str(value), quote=True)


def link(path, text):
    return f'<a href="{esc(quote(path, safe="/#-._"))}">{esc(text)}</a>'


def badge(status):
    color = {"PASS": "success", "FAIL": "error", "REFUSED": "warning", "BLOCKED": "warning"}.get(status, "neutral")
    return f'<span class="badge {color}">{esc(status)}</span>'


def references(refs):
    if not refs:
        return '<p>无取证记录</p>'
    items = []
    for ref in refs:
        text = ref["suite"] + (f' · {ref["test"]}' if ref.get("test") else "")
        evidence = link("#suite-" + ref["suite"], text) if ref["linked"] else esc(text)
        scope = SCOPES_ZH.get(ref["scope"], "无记录")
        if ref["phase"] == "supporting":
            scope += " · 独立运行"
        marker = f'<q>{esc(ref["contains"])}</q>' if ref.get("contains") else ""
        reason = f' · {esc(ref["reason"])}' if ref["reason"] else ""
        items.append(f'<li>{badge(ref["status"])} {evidence} · {esc(scope)}{reason}{marker}</li>')
    return "<ul>" + "".join(items) + "</ul>"


def render_acceptance(row, artifacts):
    checks = []
    for item in row["checks"]:
        steps = "".join(f"<li>{esc(step)}</li>" for step in item.get("steps", []))
        links = " · ".join(link("#artifact-" + key, artifacts[key].get("caption", key)) for key in item.get("artifacts", []))
        checks.append(f'<div class="check">{badge(item["status"])}<dl>'
                      f'<dt>预期</dt><dd>{esc(item["expected"])}</dd><dt>实际</dt><dd>{esc(item["actual"])}</dd></dl>'
                      f'{"<ol>" + steps + "</ol>" if steps else ""}{references(item["evidence"])}'
                      f'{"<p>" + links + "</p>" if links else ""}</div>')
    reason = row["reason"] or ("尚无验收记录" if not checks else "")
    note = f"<p>{esc(reason)}</p>" if reason else ""
    return (f'<details id="requirement-{row["id"]}" data-result="{esc(row["status"])}"><summary>'
            f'{badge(row["status"])} <strong>{row["id"]}.</strong> {esc(row["criterion"])}</summary>'
            f'{note}{"".join(checks)}</details>')


def render_operation(title, row):
    fields = (
        ("阶段", "operation"), ("环境 / 版本", "version"), ("操作 ID", "operation_id"),
        ("当前镜像", "source_image"), ("目标镜像", "target_image"), ("实际结果", "detail"),
    )
    body = "".join(f'<dt>{label}</dt><dd>{esc(row[key])}</dd>' for label, key in fields if row.get(key))
    requested = "" if row["status"] == row["reported_status"] else f'<p>录入状态 {esc(row["reported_status"])}，缺少对应验证。</p>'
    return (f'<details><summary>{badge(row["status"])} {esc(title)}</summary>'
            f'<dl>{body}</dl>{requested}{references(row["evidence"])}</details>')


def render_suite(suite):
    cases = "".join(f'<li>{badge(case["status"])} <strong>{esc(case["test"])}</strong> · {esc(case["package"])}'
                    f' · {esc(case["elapsed_seconds"] if case["elapsed_seconds"] is not None else "—")} s</li>'
                    for case in suite["cases"])
    pkgs = "".join(f'<li>{badge(package["status"])} {esc(package["package"])}</li>' for package in suite["packages"])
    fields = (
        ("工作目录", suite["cwd"]), ("阶段与运行标签", suite["phase"] + " / " + suite["run_label"]),
        ("开始", suite["started_at"]), ("结束", suite.get("finished_at", "—")),
        ("耗时（秒）", suite.get("elapsed_seconds", "—")), ("命令退出码", suite["exit_code"]),
        ("日志 SHA-256", suite.get("evidence_sha256", "—")), ("Go JSON 无效行", suite.get("malformed_lines", "—")),
    )
    if suite.get("published_log_sha256"):
        fields += (("交付副本 SHA-256（凭据已脱敏）", suite["published_log_sha256"]),)
    metadata = "".join(f"<dt>{key}</dt><dd>{esc(value)}</dd>" for key, value in fields)
    link_text = link(suite["log"], "查看原始日志") + " · " + link(suite["record"], "查看采集记录")
    return (f'<details id="suite-{esc(suite["suite_id"])}"><summary>{badge(suite["status"])} '
            f'{esc(suite["name"])} · {esc(SCOPES_ZH[suite["scope"]])}</summary><dl>{metadata}</dl>'
            f'<pre>{esc(suite["command"])}</pre><p>{link_text}</p>'
            f'<p>{esc(suite.get("reason", ""))}</p>'
            f'<details><summary>日志末尾（最多 {PREVIEW_CHARS} 字符）</summary><pre>{esc(suite["output_preview"])}</pre></details>'
            f'{"<details><summary>Go 包 " + str(len(suite["packages"])) + "</summary><ul>" + pkgs + "</ul></details>" if pkgs else ""}'
            f'{"<details><summary>Go 测试事件 " + str(len(suite["cases"])) + "</summary><ul>" + cases + "</ul></details>" if cases else ""}</details>')


def render_supporting(run):
    fields = (("原始版本", "source_revision"), ("源码 SHA-256", "source_manifest_sha256"), ("工作目录", "cwd"),
              ("开始", "started_at"), ("结束", "finished_at"), ("耗时（秒）", "elapsed_seconds"),
              ("退出码", "exit_code"), ("日志 SHA-256", "evidence_sha256"),
              ("交付副本 SHA-256（凭据已脱敏）", "published_log_sha256"), ("环境与范围", "detail"))
    metadata = "".join(f"<dt>{label}</dt><dd>{esc(run[key])}</dd>" for label, key in fields if run.get(key) is not None)
    return (f'<details id="suite-{esc(run["suite_id"])}"><summary>{badge(run["status"])} '
            f'{esc(run.get("name", run["suite_id"]))} · {esc(SCOPES_ZH[run["scope"]])}</summary><dl>{metadata}</dl>'
            f'<pre>{esc(run["command"])}</pre><p>{link(run["log"], "查看原始证据")}</p>'
            f'<details><summary>证据末尾（最多 {PREVIEW_CHARS} 字符）</summary><pre>{esc(run["output_preview"])}</pre></details></details>')


def render_artifact(name, item, file):
    caption = item.get("caption", name)
    raw = file.read_bytes()
    image_types = {".png": (b"\x89PNG\r\n\x1a\n", "png"), ".jpg": (b"\xff\xd8\xff", "jpeg"),
                   ".jpeg": (b"\xff\xd8\xff", "jpeg")}
    signature, extension = image_types.get(file.suffix.lower(), (None, None))
    image = ""
    if signature and raw.startswith(signature):
        data = base64.b64encode(raw).decode("ascii")
        image = f'<img loading="lazy" src="data:image/{extension};base64,{data}" alt="{esc(caption)}">'
    scope = SCOPES_ZH.get(item.get("scope"), item.get("scope", ""))
    details = " · ".join(esc(value) for value in (scope, item.get("captured_at")) if value)
    return (f'<figure id="artifact-{esc(name)}"><figcaption>{link(item["link"], caption)}{" · " + details if details else ""}'
            f'</figcaption>{image}</figure>')


CSS = """
:root{color-scheme:light;font-family:"Segoe UI",system-ui,sans-serif;color:#242424;background:#f5f5f5}
*,*::before,*::after{box-sizing:border-box}body{margin:0;line-height:1.55}header,main{width:min(1100px,100%);margin:auto;padding:24px}
header{padding-bottom:0}h1{font-size:clamp(1.45rem,4vw,2rem);line-height:1.2;margin:12px 0}h2{font-size:1.2rem;margin:0 0 12px}
header p{margin:8px 0}time{display:inline-block}section{padding:20px;margin:0 0 16px;background:#fff;border:1px solid #e0e0e0;border-radius:8px}
nav,.metrics,.filters{display:flex;flex-wrap:wrap;gap:8px 16px;align-items:center}
nav{margin:12px 0}nav a{padding:5px 0}.muted{color:#616161}.badge{display:inline-block;white-space:nowrap;font-size:.8rem;
font-weight:650;line-height:1.45;border-radius:4px;padding:2px 7px;background:#f5f5f5;color:#424242}
.badge.success{background:#f1faf1;color:#0e700e}.badge.error{background:#fdf3f4;color:#b10e1c}
.badge.warning{background:#fff4e5;color:#8a3700}.badge.neutral{background:#f0f0f0}
a{color:#0f6cbd;text-underline-offset:3px}a:hover{color:#115ea3}a:focus-visible,summary:focus-visible,input:focus-visible,select:focus-visible{outline:2px solid #0f6cbd;outline-offset:2px}
details{border:1px solid #e0e0e0;border-radius:4px;margin:8px 0;padding:0 10px}details[open],details:target{border-color:#a9c9e6}
summary{cursor:pointer;padding:9px 2px;overflow-wrap:anywhere}summary:hover{color:#115ea3}
.check{padding:12px 0;border-top:1px solid #e0e0e0}dl{display:grid;grid-template-columns:minmax(7em,12em) 1fr;gap:4px 12px;margin:10px 0}
dt{font-weight:600}dd{margin:0;min-width:0;overflow-wrap:anywhere}q{display:block;color:#424242;overflow-wrap:anywhere}
p,li,figcaption{overflow-wrap:anywhere}ul,ol{padding-inline-start:1.5rem}li{margin:.25rem 0}
pre{font-size:.85rem;max-height:28rem;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;background:#fafafa;border:1px solid #e0e0e0;border-radius:4px;padding:10px}
figure{margin:12px 0}img{display:block;max-width:100%;height:auto;border:1px solid #e0e0e0;border-radius:4px;margin:8px 0}
input,select{font:inherit;min-height:36px;max-width:100%;padding:4px 8px;border:1px solid #616161;border-radius:4px;background:#fff;color:#242424}
:target{scroll-margin:16px}.summary{margin:12px 0}.download{font-weight:600}
@media(max-width:520px){header,main{padding:16px}header{padding-bottom:0}section{padding:12px}dl{display:block}dd{margin-bottom:8px}
.filters{display:grid;grid-template-columns:auto minmax(0,1fr)}.filters input,.filters select{min-height:44px}
.filters [role=status]{grid-column:1/-1}summary{min-height:44px}}
@media(forced-colors:active){summary:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible{outline-color:Highlight}}
"""

SCRIPT = """
const filter=document.querySelector('#filter-status');
const query=document.querySelector('#filter-query');
const rows=[...document.querySelectorAll('#matrix details[data-result]')];
function update(){
  const term=query.value.trim().toLowerCase();
  let shown=0;
  for(const row of rows){
    row.hidden=Boolean((filter.value && row.dataset.result!==filter.value) ||
                       (term && !row.textContent.toLowerCase().includes(term)));
    if(!row.hidden) shown++;
  }
  document.querySelector('#filter-count').textContent='显示 '+shown+' / '+rows.length;
}
function reveal(){
  const target=document.getElementById(location.hash.slice(1));
  if(target instanceof HTMLDetailsElement) target.open=true;
}
filter.addEventListener('change',update);
query.addEventListener('input',update);
addEventListener('hashchange',reveal);
update();
reveal();
"""


def render_html(data, files, basename, bundle):
    def tally(counts):
        return '<div class="metrics">' + "".join(f'<span>{badge(status)} <strong>{counts.get(status, 0)}</strong></span>'
                                                   for status in STATUSES) + '</div>'

    title = "Runtime slot 重置与容量隔离 · 功能测试报告"
    label = "示例夹具" if data["sample"] else "最终冻结运行"
    verdict = badge(data["acceptance_status"])
    if data["sample"]:
        verdict = badge("示例夹具")
    downloads = link(f"{basename}.json", "报告 JSON") + " · " + link("source-manifest.json", "源码 SHA-256 清单")
    if bundle:
        downloads += " · " + link(data["bundle"], "下载证据 ZIP")
    matrix = "".join(render_acceptance(row, data["artifacts"]) for row in data["acceptance"])
    upgrades = "".join(render_operation(name, data["native_upgrades"][kind]) for kind, name in RUNTIMES.items())
    runtimes = "".join(render_operation(name, data["runtime_validation"][kind]) for kind, name in RUNTIMES.items())
    suites = "".join(render_suite(row) for row in data["suites"])
    supporting = "".join(render_supporting(row) for row in data["supporting_runs"])
    supporting_section = f'<section id="supporting"><h2>独立版本和先前运行的证据</h2>{supporting}</section>' if supporting else ""
    artifacts = "".join(render_artifact(name, item, files[item["link"]]) for name, item in data["artifacts"].items())
    required = " ".join(f'<li>{badge(status)} {esc(name)}</li>' for name, status in data["required_suites"].items())
    notes = "".join(f"<li>{esc(note)}</li>" for note in data["limitations"])
    regression_items = "".join(
        "<li>" + esc(item["problem"]) + " · " + esc(item["before"]) + " · " + esc(item["after"]) + "</li>"
        for item in data["regressions"]
    )
    env = esc(json.dumps(data["environment"], indent=2, ensure_ascii=False))
    source = esc(json.dumps(data["source"], indent=2, ensure_ascii=False))
    options = "".join(f'<option>{esc(status)}</option>' for status in STATUSES)
    return f'''<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title><style>{CSS}</style></head>
<body><header><p class="muted">ClawManager · {label}</p><h1>{title}</h1>
<p>{verdict} · {esc(data["run_label"])} · 冻结于 <time>{esc(data["code_frozen_at"])}</time></p>
<nav aria-label="报告目录"><a href="#matrix">验收矩阵</a><a href="#operations">部署与原生升级</a><a href="#suites">执行记录</a><a href="#artifacts">截图与证据</a></nav>
<p class="download">{downloads}</p></header><main>
<section id="matrix"><h2>验收矩阵</h2>{tally(data["acceptance_counts"])}
<p class="filters"><label for="filter-status">结果</label><select id="filter-status"><option value="">全部</option>{options}</select>
<label for="filter-query">搜索</label><input type="search" id="filter-query"><span id="filter-count" role="status"></span></p>{matrix}</section>
<section id="operations"><h2>部署与原生升级</h2>{render_operation("CM 应用部署", data["deployment"])}
<h3>四类 Lite Runtime 功能验证</h3>{runtimes}<h3>四类 Runtime 原生升级</h3>{upgrades}</section>
<section id="suites"><h2>执行记录</h2><p>记录的套件</p>{tally(data["suite_counts"])}
<p>Go 测试结果（含子用例）</p>{tally(data["test_counts"])}
<details><summary>必需套件</summary><ul>{required}</ul></details>{suites}</section>{supporting_section}
<section id="artifacts"><h2>截图与证据</h2>{artifacts or '<p>没有登记附件。</p>'}</section>
<section><h2>环境与版本</h2><pre>{env}</pre><pre>{source}</pre></section>
<section><h2>限制与失败修复</h2><ul>{notes}{regression_items}</ul></section></main><script>{SCRIPT}</script></body></html>'''


def generate(manifest, output, basename, bundle):
    if not IDENTIFIER.fullmatch(basename):
        raise ValueError("invalid report basename")
    data, files = build_report(manifest.resolve(), basename)
    if not bundle:
        data["bundle"] = None
    output.mkdir(parents=True, exist_ok=True)
    fingerprints = {"source-manifest.json": data["source"]["manifest_sha256"]}
    fingerprints.update((run["log"], run.get("evidence_sha256")) for run in data["suites"] + data["supporting_runs"])
    fingerprints.update((artifact["link"], artifact["sha256"]) for artifact in data["artifacts"].values())
    runs = {run["log"]: run for run in data["suites"] + data["supporting_runs"]}
    for name, source in files.items():
        target = output / name
        target.parent.mkdir(parents=True, exist_ok=True)
        observed = source.read_bytes()
        first_party_text = name in runs or name.startswith("records/") or name == "input-manifest.json"
        published = observed
        if first_party_text:
            original_text = observed.decode("utf-8", errors="replace")
            safe_text = redact(original_text)
            if safe_text != original_text:
                published = safe_text.encode("utf-8")
        expected = fingerprints.get(name)
        if expected and digest(observed) != expected:
            raise ValueError(f"artifact changed during report generation: {name}")
        if published != observed:
            if name in runs:
                runs[name]["published_log_sha256"] = digest(published)
            if source.resolve() == target.resolve():
                raise ValueError(f"report output would overwrite original evidence: {name}")
        if source.resolve() != target.resolve():
            target.write_bytes(published)
    if any(run.get("published_log_sha256") for run in runs.values()):
        data["limitations"].append("交付日志和报告预览已去除凭据片段；保留原始采集 SHA-256 与脱敏副本 SHA-256，原始日志留在 54。")
    safe_json = redact(json.dumps(data, ensure_ascii=False, indent=2, allow_nan=False))
    data = json.loads(safe_json)
    for name, contents in (
        (basename + ".json", safe_json + "\n"),
        (basename + ".html", render_html(data, {name: output / name for name in files}, basename, bundle)),
    ):
        (output / name).write_text(contents, encoding="utf-8")
        files[name] = output / name
    if bundle:
        with zipfile.ZipFile(output / data["bundle"], "w", compression=zipfile.ZIP_DEFLATED) as archive:
            lines = []
            for name in sorted(files):
                raw = (output / name).read_bytes()
                archive.writestr(name, raw)
                lines.append(digest(raw) + "  " + name)
            archive.writestr("SHA256SUMS", "\n".join(lines) + "\n")
    return data


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--out-dir", required=True, type=Path)
    parser.add_argument("--basename", default="2026-11-18-slot-reset-functional-test-report")
    parser.add_argument("--bundle", action="store_true")
    args = parser.parse_args()
    try:
        data = generate(args.manifest, args.out_dir, args.basename, args.bundle)
    except (OSError, KeyError, ValueError, TypeError, AttributeError) as error:
        parser.error(str(error))
    print(json.dumps({key: data[key] for key in ("accepted", "acceptance_status", "acceptance_counts", "suite_counts")},
                     ensure_ascii=False))


if __name__ == "__main__":
    main()
