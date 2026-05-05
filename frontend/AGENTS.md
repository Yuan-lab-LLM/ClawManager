# frontend/AGENTS.md - Frontend Project Rules

> This file applies to `frontend/`. Root `../AGENTS.md` and `../.specify/memory/constitution.md` still apply.

## Scope

The frontend is the React 19 + TypeScript admin portal and user portal. Product-facing GTManager branding and default Chinese localization should be implemented here through the existing i18n and static-asset surfaces after the relevant spec, plan, and tasks are approved.

## Structure

- `src/lib/i18n.ts` - translation resources and locale defaults
- `src/contexts/I18nContext.tsx` - locale state, storage, and document language handling
- `src/router/` - route definitions
- `src/pages/` - route-level pages
- `src/components/` - shared UI components
- `src/services/` - API clients
- `src/stores/` - Zustand stores
- `public/` - static assets

## Commands

Run from `frontend/`:

```bash
npm ci
npm run lint
npm run build
npm run dev
```

Current `package.json` does not define `npm test`. Do not add or require a test script unless an approved feature plan adds a concrete frontend test tool.

These checks are prerequisite evidence only. UI-facing features still require Playwright E2E or recorded human E2E feedback before any `passes:true`, Close, complete, accepted, or passed claim.

## Localization and Branding Rules

- Use the existing i18n system; do not introduce a second localization framework.
- Product-facing manager wording may become GTManager only through an approved feature scope.
- Preserve OpenClaw runtime terminology where it names runtime behavior, `.openclaw` archives, OpenClaw resource management, import/export/bootstrap, or runtime status.
- Preserve protected technical identifiers such as API paths, storage keys, Kubernetes names, image names, database names, and historical evidence references unless a separate approved plan changes them.
- Do not guess logo, favicon, or loading asset paths. Asset replacement must wait for exact user-provided paths or files.

## UI Quality Rules

- Keep TypeScript strictness and ESLint clean.
- Route user-visible hardcoded strings through i18n when changing localization scope.
- Browser title, document language, logo alt text, and favicon behavior are part of E2E evidence for branding/localization features.
- Validate both clean-profile default locale behavior and explicit stored locale preservation.
