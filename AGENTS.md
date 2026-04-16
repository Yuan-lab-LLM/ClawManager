# AGENTS.md — ClawManager Project Rules

> This file is the **entry-point project constitution** for AI agents and human contributors.
> Read this file first before making any changes to the codebase.
> Deeper `AGENTS.md` files inside sub-directories take precedence over this file.

## Project Overview

**ClawManager** is a Kubernetes-first control plane for managing OpenClaw and Linux desktop runtimes at team and cluster scale.

- **Backend**: Go 1.21+ (`backend/`)
- **Frontend**: React 19 + TypeScript (`frontend/`)
- **Deployment**: K3S (primary) / K8S (`deployments/k3s/` and `deployments/k8s/`)
- **Database**: MySQL 8.x (managed via K3S deployment)
- **Object Storage**: MinIO (K3S) or EmptyDir fallback

## Directory Map

```
ClawManager/
├── backend/            # Go backend service (REST API, K8S client, AI Gateway)
├── frontend/           # React 19 frontend (admin portal + user portal)
├── deployments/
│   ├── k3s/            # Primary deployment target (local/edge)
│   └── k8s/            # Full K8S deployment
├── docs/               # Documentation
│   └── spec-kit/       # Spec-Kit quick-start and workflow guides
├── .specify/           # Feature delivery layer (spec → plan → tasks)
│   ├── memory/
│   │   └── constitution.md   # Engineering constitution
│   ├── scripts/bash/         # Feature scaffolding scripts
│   └── templates/            # spec/plan/tasks templates
├── .codex/
│   └── prompts/        # Codex /speckit.* commands
├── scripts/
│   └── codex           # One-line Codex launcher
├── AgentTeam/          # Multi-agent collaboration SOPs
├── longterm/           # Project memory layer
│   ├── workspace/
│   │   ├── app_spec.md         # Project stable facts
│   │   ├── feature_list.json   # Backlog and progress
│   │   └── claude-progress.txt # Session handoff notes
│   └── CHECKLIST.md    # Per-session checklist
└── specs/              # Feature delivery artifacts (created per feature)
    └── <feature>/
        ├── spec.md
        ├── plan.md
        └── tasks.md
```

## Sub-project Conventions

Each sub-project root (containing `package.json`, `go.mod`, etc.) must have its own `AGENTS.md`:
- `backend/AGENTS.md` — Go conventions, test commands, package structure
- `frontend/AGENTS.md` — React/TypeScript conventions, test commands, component structure

## Key Commands

```bash
# Backend (Go)
cd backend
go build ./...
go test ./...

# Frontend (React)
cd frontend
npm install
npm run dev
npm test

# K3S local deployment (via k3d)
k3d cluster create clawmanager --port "30443:30443@loadbalancer"
kubectl apply -f deployments/k3s/clawmanager.yaml

# Spec-Kit workflow
./scripts/codex                                                    # Launch Codex
./.specify/scripts/bash/create-new-feature.sh "feature" --short-name xxx
./.specify/scripts/bash/setup-plan.sh
```

## Development Workflow

See `.specify/memory/constitution.md` for full engineering rules.

Short version:
1. Read `longterm/workspace/app_spec.md` and `feature_list.json`
2. Pick an unfinished feature (`passes: false`)
3. Run `./.specify/scripts/bash/create-new-feature.sh`
4. Fill `specs/<feature>/spec.md` → `plan.md` → `tasks.md`
5. Implement step-by-step, run tests, commit
6. Update `longterm/workspace/feature_list.json` (`passes: true`) and `claude-progress.txt`

## Forbidden Actions

- Do NOT commit `.codex/auth.json` or `.codex/config.toml`
- Do NOT directly modify `deployments/k3s/clawmanager.yaml` without updating `docs/`
- Do NOT hardcode secrets — use Kubernetes Secrets or environment variables

## Authority Hierarchy

1. This `AGENTS.md` — project entry rules
2. `.specify/memory/constitution.md` — engineering quality gates
3. `longterm/workspace/app_spec.md` — project stable facts (what, why, scope)
4. `longterm/workspace/feature_list.json` — backlog truth source
5. `specs/<feature>/` — current feature delivery artifacts

## Memory Operating Rules

- Before starting any new task, run `search_memories` for project facts, active decisions, and relevant `session_state`.
- After important work completes, write or update durable memory for validated facts, decisions, anti-patterns, preferences, or environment findings.
- Write only distilled durable facts; do not store chat, long logs, command echo, or full legacy files.
- When a new fact replaces an old memory, prefer `update_memory` over adding a duplicate.
- Do not paste `longterm/`, `AgentTeam/`, or `docs/superpowers/` files into Mem0 verbatim; extract the most specific actionable memory instead.
- When context is about to be lost, a packet closes with active blockers, or a session stops mid-stream, write `session_state`.
