# 中文路由分类对照评测

仅使用合成问题，直接调用 Gateway 的真实规则实现与本地 Laya 多语言 checkpoint；不访问运行中的 ClawManager、业务数据库、供应商 API 或真实会话，不修改正式路由。

`cases.zh.json` 的 40 条参考档位由助手在推理前按项目政策标注，尚未经过独立人工复核。每档 10 条；core/challenge 各 20 条。不能把此开发测试集成绩解释成生产准确率，也不能用它调参后仍宣称是独立测试结果。

## 准备和运行

以下为历史 0.3.20 评测的复现方法；当前部署 0.3.21 的提示词消融实验见本文末尾，不要将两轮成绩混用。

使用 Python 3.12 独立虚拟环境（例如项目的 `.codex-temp/laya-eval-venv`）。安装 CPU 版 torch 和固定版本 `laya==0.3.20`；其余依赖版本会记录在测试环境，报告记录主要框架版本。

```powershell
py -3.12 -m venv .codex-temp/laya-eval-venv
.\.codex-temp\laya-eval-venv\Scripts\python.exe -m pip install torch --index-url https://download.pytorch.org/whl/cpu
.\.codex-temp\laya-eval-venv\Scripts\python.exe -m pip install laya==0.3.20 --index-url https://pypi.org/simple
```

本机首次下载遇到 Xet 吞吐下降，改用同一官方源的普通 HTTP 完成续传；如遇同样情况，可在当前 PowerShell 会话设置 `$env:HF_HUB_DISABLE_XET='1'` 后运行下载。首次测试解析的权重 revision 为 `e4e9ddf21a7b1903b7acffd8814ad4307bf63a67`；复现应固定这个 revision，而不是依赖不断变化的 main。

1. 在 backend 下运行真实规则评测，输出路径必须尚不存在：

   ```powershell
   go run ./cmd/routing-eval -cases ../e2e/routing-classifier/cases.zh.json -output ../.codex-temp/routing-rules-20260924.json
   ```

2. 回到仓库根目录运行 Laya 对照。`--download` 显式允许从 Hugging Face 获取公开模型，模型缓存留在项目内，不使用共享模型缓存。输出目录必须尚不存在。

   ```powershell
   .\.codex-temp\laya-eval-venv\Scripts\python.exe e2e/routing-classifier/compare_laya.py --cases e2e/routing-classifier/cases.zh.json --rules .codex-temp/routing-rules-20260924.json --cache .codex-temp/laya-model-cache --output outputs/laya-routing-20260924-01 --download
   ```

3. 首次下载解析并记录确切权重 revision。离线复现时删除 `--download`，用报告中的 SHA 指定 `--revision`，并使用新的输出目录。CPU 默认四线程，模型预热后顺序推理，不改变 K8s 资源配置。

生成 `report.md`、`results.json` 和逐题追加的 `partial.jsonl`。下载/加载错误使程序失败，不产生伪造的模型成绩；逐题推理失败计入分母，不以规则结果补齐。若中途失败保留现场，重试用新的输出目录，不覆盖已完成结果。

## 公平性与局限

- 两者接收相同的用户文本；Laya 接收固定中文分档标准，规则对应同一政策。Laya 不接收参考档位、备注或题组名称。
- 当前语料均是短文本；模型输入与问题定义需通过长度预检，避免静默截断。未评测长上下文、图片、工具协议或线上负载。
- 规则每题预热后测量 5 批，每批至少 20ms，按调用次数均摊再取批次中位数，避免 Windows 快速调用的零值计时；Laya 预热三次后每题测一次。仅测本地分类耗时，不含进程启动、下载、网络和正式模型回答。
- `--reuse-laya-results <原 results.json>` 可重用已归档的模型推理，重新汇总规则计时。程序要求题集哈希、分类说明、题目 ID 和规则档位完全相同，不能将改过规则的结果冒充仅重测耗时。
- `confidence` 和最大选项概率不是经过本任务验证的正确率，不据此事后筛掉错误样本。
- 重点看挑战题、各档混淆、低估次数，不只看总一致率。参考档位政策将形式化证明列为 4 档，即使某道证明对人来说很简单；这不是客观难度评估。
- 评测不会安装或启用 Gateway 的 Laya 适配器。是否接入正式路由需另行决定。

测试评测统计逻辑（无需加载模型）：

```powershell
py -3.12 -m unittest discover -s e2e/routing-classifier -p test_compare.py
```

## 当前部署镜像：提示词消融实验

2026-09-28 已完成的 305 次推理结果与结论见 [实测报告](../../docs/laya-prompt-ablation-20260928.md)。短说明修正了用户报告的排序题，但未据此替换正式提示词。

`prompt_ablation.py` 固定五组配置，A 为管理端当前 v2；B 只缩短说明；C 只替换为具体任务选项；D 只将 JSON 用户轮次改为纯文本（保留历史顺序和当前任务标记）；E 组合 B/C/D。选项标签和顺序始终不变。不在看到结果后继续改写提示词。B 改变了说明的内容和长度，不能把差异全部解释成纯粹的 token 长度效应。

题集：原 40 条开发题、`prompt-holdout.zh.json` 的 20 条新合成题（每档 5 条，含英文与续问）、单独一条用户报告的排序题。参考档位和配置在模型加载前写入 `frozen.json`；仅用户文本送入模型，参考标签和题目 ID 不送入。所谓新题不是第三方独立盲测，也不能用它选完提示词后仍声称是独立验证集。

运行使用已经存在的 `10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1` 本地镜像，不需要服务 API Key，不访问远端。以下为仓库在 `D:/ClawManager` 时的例子；每轮使用新输出目录，不覆盖已有报告：

```powershell
New-Item -ItemType Directory -Force outputs/laya-prompt-ablation-20260928
docker --context desktop-linux run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --memory 3g --cpus 2 --tmpfs /tmp:rw,size=256m -e HF_HUB_OFFLINE=1 -e TRANSFORMERS_OFFLINE=1 --mount type=bind,source=D:/ClawManager/e2e/routing-classifier,target=/work/eval,readonly --mount type=bind,source=D:/ClawManager/deployments/laya,target=/work/client,readonly --mount type=bind,source=D:/ClawManager/outputs/laya-prompt-ablation-20260928,target=/work/output --entrypoint python 10.130.14.23:5000/laya-hsr:cpu-offline-20260928-v1 /work/eval/prompt_ablation.py --baseline-client /work/client/call_laya.py --model /opt/models/laya-multilingual --output /work/output/run-01 --threads 2
```

内部调用同镜像的 `Agent.predict`，服务原本显式固定 multilingual 且不传 lang，Router 最终也调用该 Agent 的 system_one；不经过 HTTP，耗时不能代替远端服务延迟。检查完整选项不超过 48 tokens、整个 head 不超过 512、输入不被截断；固定总上限 2048。各版本分别预热一次、每题单次推理，顺序轮换以减弱先后偏差；不批处理、不重试失败、不用规则修正模型结果。

加入真实 v4 规则基准（仓库根目录执行；Laya 推理期间即可先执行前两步）：

```powershell
python e2e/routing-classifier/summarize_ablation.py --run outputs/laya-prompt-ablation-20260928/run-01 --export-rules-input
go -C backend run ./cmd/routing-eval -cases ../outputs/laya-prompt-ablation-20260928/run-01/rules-input.json -output ../outputs/laya-prompt-ablation-20260928/run-01/rules.json
python e2e/routing-classifier/summarize_ablation.py --run outputs/laya-prompt-ablation-20260928/run-01
```

查看 `comparison-with-rules.md`；`results.json` 含四档概率、分布集中度、最高概率、前两名差距、耗时及混淆矩阵；`comparison-with-rules.json` 列出相对 v2 修正及退步的题目 ID。失败计入分母。不自动选取“获胜”版本，不更新生产提示词，不改变路由开关或 K8s。

脚本回归测试（不加载模型）：

```powershell
python -m unittest discover -s e2e/routing-classifier -p test_prompt_ablation.py
```
