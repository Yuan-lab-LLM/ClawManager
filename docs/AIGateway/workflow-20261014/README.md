# Gateway 操作指南图片

这些图片由独立操作指南 `docs/aigateway-operation-guide_cn.md` 和 `docs/aigateway-routing-governance_cn.md` 第 18 节共同引用。

## 来源

- `01`、`02`、`03`、`04`、`04b`、`05`、`06`、`08`、`09`、`10`、`11`：2026-10-14 本机 `https://localhost:30443` 真实管理界面截图。`03` 和 `06` 是未提交表单；`04b` 只改变浏览器表单选择，未启用 Hybrid。
- `07-instance-model-hint-historical-20260929.png`：用户在此前对话中提供的截图，原文件为 `codex-clipboard-39GCWf.png`。保留其原有红框，用于说明实例最近调用提示的位置。不是本次新生成的回答。
- `flow-01-user-journey`：使用全流程。
- `flow-02-auto-routing`：Auto / Hybrid 分类和模型选择流程。
- `flow-03-governance`：Key 准入、预算、部署保护和结束结算流程。
- 三张流程图均包含 SVG 源文件和本地浏览器渲染的 PNG。流程图是机制说明，不是运行日志或性能测试结果。

本次截图时模型目录为空，“我的实例”为空，分类器显示本地规则模式。没有保存设置、创建实例、调用模型或执行 Laya 对照。灰色实心块为密码输入框的截图遮罩，不表示该字段一定已有凭据。

## 重拍

只读采集脚本为 `docs/tools/capture-gateway-guide.cjs`，使用独立无界面浏览器会话，不读取用户日常浏览器资料，不持久化登录状态。

需要本地可用的 Playwright 包和 Microsoft Edge。默认从 `e2e/node_modules/playwright` 加载，也可用 `GUIDE_PLAYWRIGHT_PATH` 指定已有安装的 `playwright/package.json`。默认尝试项目 E2E 测试账号一次；其他环境请通过 `GUIDE_USERNAME`、`GUIDE_PASSWORD` 环境变量提供测试账号，不要将真实密码写进脚本或本文。

```powershell
# 从项目根目录运行。要求 Playwright 和 Edge 已安装。
node docs/tools/capture-gateway-guide.cjs inspect
node docs/tools/capture-gateway-guide.cjs capture
node docs/tools/capture-gateway-guide.cjs supplement
node docs/tools/capture-gateway-guide.cjs diagrams
```

脚本在登录后阻止浏览器页面的非 GET/HEAD/OPTIONS 请求；仅打开新增表单或切换未保存的模式。它不会通过测试框架运行数据库重置、保存模型、创建实例或调用外部分类型服务。

当前输出目录固定为本目录。同名重拍会覆盖对应图片；要保存其他日期的截图，请先修改脚本的输出目录并同步更新文档说明。历史截图不会由脚本重新生成。

如果未来环境有真实用户数据，重拍前应检查页面中是否包含会话文本、个人信息、业务地址或密钥。脚本自动遮蔽密码输入和嵌入式 iframe，但不能代替对所有页面内容的人工隐私审查。

## 分享

请同时发送 Markdown 文档和此目录，保持相对路径不变。仅发送 `.md` 文件会丢失截图。PNG 可直接放入汇报材料，SVG 可用于后续编辑流程节点和文字。
