#!/usr/bin/env python3
"""Render final, recorded functional-test evidence into an offline HTML report."""
import argparse
import base64
from collections import Counter
from datetime import datetime
import hashlib
import html
import json
import math
from pathlib import Path
import re

REQUIRED_COVERAGE = {
    "queries": "查询与分页", "large_tables": "大表维护", "storage_dedup": "正文去重与存储",
    "redis_bounds": "Redis 容量与回收", "E1_fee": "E1 费用正确性", "E2_audit": "E2 审计完整性",
    "E3_projection": "E3 原子投影", "E4_durable": "E4 持久补偿", "E5_redis_reads": "E5 Redis 有界读取",
    "E6_backup_partials": "E6 未完成备份", "monthly_routing": "月表实际路由", "native_restore": "原生隔离恢复",
    "owner_authorization": "所有者权限", "frontend_workflows": "页面交互", "runtime_durability": "Runtime 故障恢复",
}
REQUIRED_PERFORMANCE = {
    "audit_default30": 500, "audit90": 500, "audit_search30": 500, "audit_search90": 2000,
    "admin_all": 2000, "admin_fractional": 2000, "session_instance": 500,
    "trace_page": 500, "cost_overview": 2000, "cost_fractional": 2000,
}
REQUIRED_SUITES = {
    "backend-full", "backend-race", "query-capacity", "audit-capacity", "frontend-types", "frontend-build",
    "browser-audit-list-pagination", "browser-cost-pagination", "browser-trace-pagination", "browser-session-refresh",
    "runtime-hermes", "runtime-opencode", "runtime-node-build-check", "runtime-faults", "redis-budget",
    "report-integrity", "backup-partial-shell", "backup-helper-stock-image",
}
PERFORMANCE_EVIDENCE = {
    "audit_default30": ("audit-capacity", "scenario=default30-equivalent"),
    "audit90": ("audit-capacity", "scenario=all90"), "audit_search90": ("audit-capacity", "scenario=search90"),
    "audit_search30": ("audit-capacity", "scenario=search30"),
    "admin_all": ("query-capacity", "instances=10000 active=100%"),
    "admin_fractional": ("query-capacity", "scenario=admin-fractional-window"),
    "session_instance": ("query-capacity", "scenario=instance-session"),
    "trace_page": ("query-capacity", "scenario=trace-page"),
    "cost_overview": ("query-capacity", "cost overview"),
    "cost_fractional": ("query-capacity", "scenario=cost-fractional-window"),
}


def duration_ms(value):
    parts = re.findall(r"(\d+(?:\.\d+)?)(ns|µs|us|ms|s|m|h)", value)
    if not parts or ''.join(number + unit for number, unit in parts) != value:
        raise ValueError("invalid Go duration")
    units = {"ns": .000001, "µs": .001, "us": .001, "ms": 1, "s": 1000, "m": 60000, "h": 3600000}
    return sum(float(number) * units[unit] for number, unit in parts)


def verified_metric(row, suites):
    expected = PERFORMANCE_EVIDENCE.get(row.get("id"))
    if expected is None:
        return False
    suite_id, marker = expected
    suite = next((s for s in suites if s.get("suite_id") == suite_id and s["kind"] == "go-json"), None)
    recorded = row.get("evidence_output", "")
    if suite is None or row.get("evidence_suite_id") != suite_id or not recorded or "MEASURED " not in recorded or marker not in recorded:
        return False
    lines = []
    for line in Path(suite["evidence"]).read_text().splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if isinstance(event, dict) and event.get("Test") == row.get("evidence_test") and event.get("Action") == "output":
            lines.extend(event.get("Output", "").splitlines())
    if recorded not in lines:
        return False
    try:
        samples = int(re.search(r"samples=(\d+)", recorded)[1])
        p50 = duration_ms(re.search(r"P50=(\S+)", recorded)[1])
        p95 = duration_ms(re.search(r"P95=(\S+)", recorded)[1])
        return samples == row["samples"] and math.isclose(p50, row["p50_ms"], abs_tol=.000001) and math.isclose(p95, row["p95_ms"], abs_tol=.000001)
    except (ValueError, TypeError, KeyError):
        return False


def escaped(value):
    return html.escape(str(value), quote=True)


def read_cases(suite):
    evidence = Path(suite["evidence"])
    raw = evidence.read_text(errors="replace")
    if suite["kind"] != "go-json":
        return [{"name": suite["name"], "suite": suite["name"], "category": suite["category"],
                 "status": "PASS" if suite.get("exit_code") == 0 else "FAIL" if suite.get("exit_code") is not None else "NOT_RUN",
                 "elapsed": suite.get("elapsed_seconds"), "output": raw[-12000:], "command": suite["command"],
                 "evidence": evidence.name, "evidence_sha256": hashlib.sha256(evidence.read_bytes()).hexdigest()}]
    cases, packages, started, malformed = {}, {}, set(), []
    for line in raw.splitlines():
        try:
            item = json.loads(line)
        except (ValueError, TypeError):
            if line.strip():
                malformed.append(line[:300])
            continue
        if not isinstance(item, dict) or not isinstance(item.get("Action"), str):
            malformed.append(line[:300])
            continue
        package, name, action = item.get("Package", ""), item.get("Test"), item.get("Action")
        if action == "start" and package:
            started.add(package)
        if name:
            case = cases.setdefault((package, name), {"name": name, "suite": suite["name"], "package": package,
                "category": suite["category"], "status": "INCOMPLETE", "output": "", "command": suite["command"], "evidence": evidence.name})
            if action == "output":
                case["output"] = (case["output"] + item.get("Output", ""))[-12000:]
            if action in ("pass", "fail", "skip"):
                case["status"], case["elapsed"] = action.upper(), item.get("Elapsed")
        elif action in ("pass", "fail", "skip"):
            packages[package] = action
    # Count leaves, not both a parent test and each of its children.
    parents = {(pkg, name.rsplit("/", 1)[0]) for pkg, name in cases if "/" in name}
    leaves = [v for key, v in cases.items() if key not in parents]
    for package, outcome in packages.items():
        if outcome == "fail" and not any(c.get("package") == package and c["status"] == "FAIL" for c in leaves):
            leaves.append({"name": package + " (package/build failure)", "suite": suite["name"], "category": suite["category"],
                "status": "FAIL", "output": raw[-12000:], "command": suite["command"], "evidence": evidence.name})
    if suite.get("exit_code") != 0 and not any(c["status"] == "FAIL" for c in leaves):
        leaves.append({"name": suite["name"] + " (incomplete command)", "suite": suite["name"], "category": suite["category"], "status": "INCOMPLETE", "output": raw[-12000:], "command": suite["command"], "evidence": evidence.name})
    if not packages:
        leaves.append({"name": suite["name"] + " (missing package completion)", "suite": suite["name"], "category": suite["category"], "status": "INCOMPLETE", "output": raw[-12000:], "command": suite["command"], "evidence": evidence.name})
    for package in started - packages.keys():
        leaves.append({"name": package + " (missing package completion)", "suite": suite["name"], "category": suite["category"], "status": "INCOMPLETE", "output": raw[-12000:], "command": suite["command"], "evidence": evidence.name})
    if malformed:
        leaves.append({"name": suite["name"] + " (malformed Go JSON evidence)", "suite": suite["name"], "category": suite["category"], "status": "INCOMPLETE", "output": "\n".join(malformed[:10]), "command": suite["command"], "evidence": evidence.name})
    digest = hashlib.sha256(evidence.read_bytes()).hexdigest()
    for case in leaves:
        case["evidence_sha256"] = digest
    return leaves


def coverage_rows(requirements, cases):
    result = []
    for requirement in requirements:
        groups = [[c for c in cases if re.search(selector, c["suite"] + " " + c["name"])] for selector in requirement["selectors"]]
        matched = {id(c): c for group in groups for c in group}.values()
        outcomes = {}
        for case in matched:
            outcomes.setdefault((case.get("package", ""), case["name"]), set()).add(case["status"])
        # A dedicated fixture/race run can cover an opt-in case skipped in the
        # broad run. A distinct skipped case, or any actual failure, cannot.
        status = "PASS" if groups and all(groups) and all("PASS" in states and not states.intersection({"FAIL", "INCOMPLETE", "NOT_RUN"}) for states in outcomes.values()) else "NOT_COVERED"
        result.append(dict(requirement, status=status, matched=len(outcomes)))
    return result


def table(headers, rows):
    return '<div class="table-wrap"><table><thead><tr>' + ''.join('<th scope="col">' + escaped(h) + '</th>' for h in headers) + '</tr></thead><tbody>' + ''.join('<tr>' + ''.join('<td>' + escaped(v) + '</td>' for v in row) + '</tr>' for row in rows) + '</tbody></table></div>'


def generate(manifest):
    if manifest.get("development_complete") is not True:
        raise ValueError("final report requires development_complete=true; preliminary runs are not final acceptance")
    frozen = datetime.fromisoformat(manifest["code_frozen_at"].replace("Z", "+00:00"))
    cases = []
    for suite in manifest["suites"]:
        started = datetime.fromisoformat(suite["started_at"].replace("Z", "+00:00"))
        if started < frozen:
            raise ValueError("final suite predates code freeze: " + suite["name"])
        if not suite.get("finished_at") or not isinstance(suite.get("elapsed_seconds"), (int, float)) or not isinstance(suite.get("exit_code"), int):
            raise ValueError("final suite lacks completion metadata: " + suite["name"])
        finished = datetime.fromisoformat(suite["finished_at"].replace("Z", "+00:00"))
        elapsed = suite["elapsed_seconds"]
        if finished < started or not math.isfinite(elapsed) or elapsed < 0 or abs((finished-started).total_seconds()-elapsed) > max(5, elapsed*.02):
            raise ValueError("final suite has inconsistent completion metadata: " + suite["name"])
        raw = Path(suite["evidence"]).read_bytes()
        if suite.get("evidence_sha256") != hashlib.sha256(raw).hexdigest() or suite.get("evidence_bytes") != len(raw):
            raise ValueError("final suite evidence differs from recording: " + suite["name"])
        expected_source = manifest.get("source", {}).get("manifest_sha256")
        if expected_source and suite.get("source_manifest_sha256") != expected_source:
            raise ValueError("final suite source differs from freeze: " + suite["name"])
        cases.extend(read_cases(suite))
    counts = Counter(c["status"] for c in cases)
    coverage = coverage_rows(manifest["coverage"], cases)
    for key, title in REQUIRED_COVERAGE.items():
        if not any(r.get("id") == key for r in coverage):
            coverage.append({"id": key, "title": title, "criterion": "缺少必需的验收证据", "matched": 0, "status": "NOT_COVERED"})
    performance = [dict(row) for row in manifest.get("performance", [])]
    for key, maximum in REQUIRED_PERFORMANCE.items():
        if not any(row.get("id") == key for row in performance):
            performance.append(dict(id=key, name=key, samples=0, p50_ms=None, p95_ms=None, limit_ms=maximum))
    for row in performance:
        if row.get("id") in REQUIRED_PERFORMANCE:
            row["limit_ms"] = min(row["limit_ms"], REQUIRED_PERFORMANCE[row["id"]])
        p95 = row.get("p95_ms")
        row["evidence_verified"] = verified_metric(row, manifest["suites"])
        row["status"] = "PASS" if row["evidence_verified"] and row.get("samples", 0) >= 20 and isinstance(p95, (int, float)) and math.isfinite(p95) and p95 <= row["limit_ms"] else "NOT_MET"
    missing_suites = sorted(REQUIRED_SUITES - {suite.get("suite_id") for suite in manifest["suites"]})
    provenance = bool(re.fullmatch(r"[0-9a-f]{64}", manifest.get("source", {}).get("manifest_sha256", ""))) and bool(re.fullmatch(r"[0-9a-f]{64}", manifest.get("evidence_bundle_sha256", "")))
    accepted = bool(counts["PASS"]) and not any(c["status"] in ("FAIL", "INCOMPLETE", "NOT_RUN") for c in cases) and all(r["status"] == "PASS" for r in coverage + performance) and manifest.get("review_passed") is True and not missing_suites and provenance
    data = dict(manifest, cases=cases, counts=dict(counts), coverage=coverage, performance=performance, missing_suites=missing_suites, accepted=accepted)
    encoded = json.dumps(data, ensure_ascii=False).replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
    sections = '<h2>功能覆盖与判定</h2>' + table(["问题 / 功能", "验证内容", "匹配用例", "结果"], [(r["title"], r["criterion"], r["matched"], r["status"]) for r in coverage])
    if missing_suites:
        sections += '<p class="FAIL">必需套件未执行：' + escaped(', '.join(missing_suites)) + '</p>'
    bundle = manifest.get("evidence_bundle", "")
    if re.fullmatch(r"[A-Za-z0-9_.-]+\.zip", bundle):
        sections += '<p><a href="' + escaped(bundle) + '">下载完整原始证据 ZIP</a>（日志展开仅展示末尾摘要；完整日志、命令、退出码、源码与证据 SHA-256 见 ZIP）</p>'
    sections += '<h2>实际性能</h2>' + table(["场景与数据量", "样本", "P50 / ms", "P95 / ms", "目标 / ms", "结果"], [(r["name"], r["samples"], r.get("p50_ms", "—"), r.get("p95_ms", "—"), r["limit_ms"], r["status"]) for r in performance])
    sections += '<h2>环境与源码</h2><pre>' + escaped(json.dumps(manifest["environment"], ensure_ascii=False, indent=2)) + '</pre><pre>' + escaped(json.dumps(manifest["source"], ensure_ascii=False, indent=2)) + '</pre>'
    sections += '<h2>修复前失败与修复后验证</h2>' + table(["问题", "修复前证据", "最终回归"], [(r["problem"], r["before"], r["after"]) for r in manifest.get("regressions", [])])
    sections += '<h2>跳过、边界与发布准备</h2><ul>' + ''.join('<li>' + escaped(item) + '</li>' for item in manifest.get("limitations", [])) + '</ul>'
    for screenshot in manifest.get("screenshots", []):
        image = base64.b64encode(Path(screenshot["path"]).read_bytes()).decode()
        sections += '<figure><img loading="lazy" src="data:image/png;base64,' + image + '" alt="' + escaped(screenshot["caption"]) + '"><figcaption>' + escaped(screenshot["caption"]) + '</figcaption></figure>'
    metrics = ''.join('<div><dt>' + escaped(label) + '</dt><dd>' + str(counts[status]) + '</dd></div>' for status, label in [("PASS", "通过"), ("FAIL", "失败"), ("SKIP", "跳过"), ("INCOMPLETE", "执行未完成")])
    page = TEMPLATE.replace("__TITLE__", escaped(manifest.get("title", "数据库与 Redis 整改功能测试报告"))).replace("__WHEN__", escaped(manifest["created_at"]))
    page = page.replace("统计按叶子用例计数，父测试不重复累加；跳过项不计为通过。", "Go 测试按叶子用例的执行次数统计，父测试不重复累加；专项及 race 重跑分别记录，其他套件按实际命令记录。跳过项不计为通过。")
    page = page.replace("个叶子用例", "条执行记录")
    page = page.replace("__VERDICT__", "验收通过" if accepted else "存在未通过或未覆盖项目").replace("__VERDICT_CLASS__", "PASS" if accepted else "FAIL")
    return page.replace("__METRICS__", metrics).replace("__SECTIONS__", sections).replace("__DATA__", encoded), data


TEMPLATE = r'''<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>__TITLE__</title><style>
:root{--ink:#242424;--text:#424242;--muted:#616161;--canvas:#f5f5f5;--surface:#fff;--border:#d1d1d1;--primary:#0f6cbd;--success:#0e700e;--error:#b10e1c}*{box-sizing:border-box}body{margin:0;background:var(--canvas);color:var(--text);font:15px/1.6 "Segoe UI",-apple-system,"PingFang SC","Microsoft YaHei",sans-serif}main{max-width:1360px;margin:auto;padding:32px}h1{font-size:28px;color:var(--ink);line-height:1.3;margin:0 0 12px}h2{font-size:21px;color:var(--ink);margin:36px 0 12px}.muted{color:var(--muted)}.verdict{font-size:18px;font-weight:600}.PASS{color:var(--success)}.FAIL,.INCOMPLETE,.NOT_RUN{color:var(--error)}.SKIP,.NOT_COVERED,.NOT_MET{color:#835b00}dl{display:flex;gap:40px;flex-wrap:wrap;margin:24px 0}dt{color:var(--muted)}dd{font-size:30px;font-weight:600;margin:0;color:var(--ink)}table{border-collapse:collapse;background:var(--surface);width:100%;table-layout:fixed}th,td{text-align:left;vertical-align:top;padding:12px;border-bottom:1px solid var(--border);overflow-wrap:anywhere}th{font-weight:600;background:#fafafa;color:var(--ink)}.table-wrap{overflow:auto;border:1px solid var(--border);border-radius:4px}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:13px/1.55 ui-monospace,SFMono-Regular,Consolas,monospace;background:var(--surface);padding:16px;border:1px solid var(--border);border-radius:4px}summary{cursor:pointer;color:var(--primary);font-weight:500}details pre{max-height:480px;overflow:auto;font-weight:400}a{color:var(--primary)}.filters{display:flex;gap:16px;align-items:end;flex-wrap:wrap;margin:16px 0}label{display:grid;gap:4px}input,select,button{font:inherit;min-height:44px;padding:8px 12px;border:1px solid var(--border);border-radius:4px;color:var(--ink);background:var(--surface)}input{min-width:240px}button{cursor:pointer}button:disabled{cursor:default;color:var(--muted)}:focus-visible{outline:2px solid var(--primary);outline-offset:3px}.pager{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:16px 0}.cases th:first-child{width:55%}figure{margin:24px 0}figure img{width:100%;height:auto;border:1px solid var(--border)}figcaption{color:var(--muted)}footer{margin-top:40px;color:var(--muted)}@media(max-width:768px){main{padding:20px 12px}h1{font-size:24px}th,td{padding:8px}dl{gap:24px}.filters label,input{width:100%;min-width:0}.filters select{width:100%}.cases th:first-child{width:45%}}@media print{body{background:white}main{padding:0}.filters,.pager,button{display:none}details pre{max-height:none}table{font-size:11px}}
</style></head><body><main><h1>__TITLE__</h1><p class="muted">开发冻结后的整合验收 · __WHEN__ · 测试集群 10.130.81.54</p><p class="verdict __VERDICT_CLASS__">__VERDICT__</p><p>统计按叶子用例计数，父测试不重复累加；跳过项不计为通过。源码、命令、日志摘要和覆盖判定均包含在报告内。</p><dl>__METRICS__</dl>__SECTIONS__<h2>完整测试明细</h2><div class="filters"><label>功能<select id="category"><option value="">全部功能</option></select></label><label>结果<select id="status"><option value="">全部结果</option><option>PASS</option><option>FAIL</option><option>SKIP</option><option>INCOMPLETE</option><option>NOT_RUN</option></select></label><label>关键词<input type="search" id="search" placeholder="查找测试名称或套件"></label><button id="download">下载结果 JSON</button></div><p id="count" role="status" aria-live="polite"></p><div class="table-wrap"><table class="cases"><thead><tr><th scope="col">测试 / 证据</th><th scope="col">功能</th><th scope="col">结果</th><th scope="col">秒</th></tr></thead><tbody id="cases"></tbody></table></div><div class="pager"><button id="prev">上一页</button><span id="page"></span><button id="next">下一页</button></div><noscript><p>功能覆盖、性能、源码和边界说明已在上方展示；完整用例筛选需要启用浏览器脚本。</p></noscript><footer>所有命令均在测试集群执行；本报告不代表应用已发布或 Runtime 已升级。</footer></main><script type="application/json" id="report-data">__DATA__</script><script>
const report=JSON.parse(document.getElementById('report-data').textContent),all=report.cases;let page=0,filtered=all;const $=id=>document.getElementById(id);for(const c of [...new Set(all.map(x=>x.category))].sort()){const o=document.createElement('option');o.value=c;o.textContent=c;$('category').append(o)}
function render(){const start=page*100,fragment=document.createDocumentFragment();for(const c of filtered.slice(start,start+100)){const tr=document.createElement('tr'),td=document.createElement('td'),d=document.createElement('details'),s=document.createElement('summary'),p=document.createElement('pre');s.textContent=c.name;p.textContent='套件：'+c.suite+'\n命令：'+c.command+'\n证据：'+c.evidence+'\n\n'+c.output;d.append(s,p);td.append(d);tr.append(td);for(const v of [c.category,c.status,c.elapsed??'—']){const t=document.createElement('td');t.textContent=v;if(v===c.status)t.className=c.status;tr.append(t)}fragment.append(tr)}$('cases').replaceChildren(fragment);$('count').textContent='匹配 '+filtered.length+' / '+all.length+' 个叶子用例';$('page').textContent='第 '+(filtered.length?page+1:0)+' / '+Math.ceil(filtered.length/100)+' 页';$('prev').disabled=page===0;$('next').disabled=start+100>=filtered.length}
function filter(){const q=$('search').value.trim().toLowerCase();filtered=all.filter(c=>(!$('category').value||c.category===$('category').value)&&(!$('status').value||c.status===$('status').value)&&(!q||(c.name+' '+c.suite).toLowerCase().includes(q)));page=0;render()}
$('category').addEventListener('change',filter);$('status').addEventListener('change',filter);let pending;$('search').addEventListener('input',()=>{clearTimeout(pending);pending=setTimeout(filter,120)});$('prev').addEventListener('click',()=>{page--;render()});$('next').addEventListener('click',()=>{page++;render()});$('download').addEventListener('click',()=>{const url=URL.createObjectURL(new Blob([JSON.stringify(report,null,2)],{type:'application/json'})),a=document.createElement('a');a.href=url;a.download='database-functional-test-results.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)});render();
</script></body></html>'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    page, data = generate(json.loads(args.manifest.read_text()))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    if data["accepted"]:
        bundle = args.output.parent / data.get("evidence_bundle", "")
        if not bundle.is_file() or hashlib.sha256(bundle.read_bytes()).hexdigest() != data.get("evidence_bundle_sha256"):
            raise ValueError("final raw evidence ZIP is absent or differs from manifest")
    args.output.write_text(page)
    args.output.with_suffix(".json").write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"accepted": data["accepted"], "counts": data["counts"], "output": str(args.output)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
