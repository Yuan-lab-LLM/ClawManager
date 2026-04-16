# Codex 快速开始（Spec‑Kit）

目的：让你不用每次手打 `export CODEX_HOME=...`，一条命令启动 Codex，并自动加载本仓库的 `/speckit.*` 命令。

## 1) 一键启动（推荐）

在仓库任意目录执行（建议从项目根目录执行）：

```bash
./scripts/codex
```

## 2) 手动启动（了解即可）

在项目根目录：

```bash
export CODEX_HOME="$PWD/.codex"
codex
```

说明：

- `CODEX_HOME` 指向项目内 `.codex`，Codex 会自动加载 `.codex/prompts/` 下的自定义命令（例如 `/speckit.tasks`）。
- 登录信息会写在 `.codex/auth.json`（已被 `.gitignore` 忽略，不会提交到仓库）。

## 3) 复用到其它项目

如果其它项目也要用同样的工作流，需要满足：

1. 项目根目录有 `.specify/` 与 `.codex/prompts/`（从模板项目复制即可）
2. 项目 `.gitignore` 忽略：
   - `.codex/auth.json`
   - `.codex/config.toml`
3. （可选）把本仓库的 `scripts/codex` 复制到新项目的 `scripts/` 下，然后同样 `./scripts/codex` 启动。

