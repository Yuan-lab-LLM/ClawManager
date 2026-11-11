# ClawManager E2E

This directory contains Playwright-based TypeScript e2e tests.

## First-Time Setup

```bash
npm ci
npx playwright install chromium
```

## Run Locally

```bash
npm run test:p0
npm run test:report
```

The default local gate is P0. P1, P2, and all tests are explicit:

```bash
npm run test:p1
npm run test:p2
npm run test:all
```

## Run Against A Target Environment

Pass only the ClawManager address. The runner defaults to the P0 suite, derives
the backend API as `/api/v1`, runs serially, skips local-only data setup, and
writes a dedicated target log.

```bash
npm run target -- 172.16.1.12:39443
```

The address may include a protocol. When omitted, `https://` is used.

## Reports

- Live progress appears in the terminal.
- Local run log: `reports/run.log`
- Target run log: `reports/target-run.log`
- JSON: `reports/results.json`
- JUnit: `reports/junit.xml`
- HTML: `playwright-report/`
- Raw artifacts: `test-results/`

## Instance workbench V4

`npm run test:workbench` runs the actual React workspace and full instance page against a disposable local integration harness. It uses real temporary Git repositories and a Python PTY, checks manual-only previews/tabs, terminal replay and retention, clone/stage/commit/push/import, generation fencing, keyboard navigation, desktop/mobile layout and shared responsive tab state. Requires Go, Git, Python 3 and locally installed Chrome. `AgentsRuntime` should be adjacent to `ClawManager`; override `WORKBENCH_RUNTIME_ROOT` otherwise. Use `WORKBENCH_SCREENSHOTS` to choose the screenshot directory. No production endpoint or account is used; all subprocesses and temporary workspaces are cleaned up. This harness does not substitute for runtime sandbox or Kubernetes migration tests.

Frontend state checks: `cd frontend && npm run test:workbench`.
