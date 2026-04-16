# Manual Skill Import Path

This path is intentionally narrow: an operator downloads a Skill `.zip` locally, imports it into ClawManager, and then attaches it to a specific OpenClaw instance.

This path does not require:

- `skill-scanner` to be enabled by default
- FastSkill auto-discovery
- `/api/v1/admin/skills` to be populated

## Where the Skill package should live

Keep the Skill `.zip` on the operator machine in any local directory, for example:

```text
/Users/<you>/Downloads/my-skill-pack.zip
```

Do not copy the archive into the repo, Pod filesystem, or Kubernetes manifests. The supported path is to upload it through the platform so ClawManager stores it in object storage.

## Archive format

- `.zip` only
- The archive must contain one or more top-level directories
- Each top-level directory becomes one skill
- Loose files at archive root are rejected

Example:

```text
my-skill-pack.zip
├── hello-skill/
│   ├── README.md
│   └── manifest.json
└── weather-skill/
    └── manifest.json
```

The archive above imports two skills: `hello-skill` and `weather-skill`.

## Import into the platform

### UI path

1. Sign in to ClawManager
2. Open `OpenClaw Resource Management`
3. Upload the `.zip` in the Skill section
4. After upload, the platform creates one or more `uploaded skill` records

### API path

Call the import route with an authenticated session or Bearer token:

```bash
curl -k \
  -X POST "https://localhost:30443/api/v1/skills/import" \
  -H "Authorization: Bearer <access-token>" \
  -F "file=@/absolute/path/to/my-skill-pack.zip"
```

## Behavior when scanner is disabled

If the default release path is running without `skill-scanner`, manual import now still succeeds.

The imported skill stays in this state:

- `source_type=uploaded`
- `scan_status=pending`
- `risk_level=unknown`

That means the package is accepted by the platform, but external scanning has not completed yet. If scanner is enabled later, the scan path can be resumed separately.

## How it reaches an instance

Import only gets the package into the platform. It does not enter an instance automatically. The next step is attach:

1. Open `Create Instance`, or an existing instance detail page
2. Select the `uploaded skill`
3. Attach it to the target instance

API route:

```bash
curl -k \
  -X POST "https://localhost:30443/api/v1/instances/<instance-id>/skills" \
  -H "Authorization: Bearer <access-token>" \
  -H "Content-Type: application/json" \
  -d '{"skill_id": <skill-id>}'
```

## What the platform does internally

The chain is:

1. ClawManager receives the local `.zip`
2. Each top-level directory becomes one `uploaded skill`
3. The platform writes `skill_blobs` and `skill_versions`
4. Attach writes `instance_skills`
5. ClawManager emits an `install_skill` command
6. The runtime agent downloads that exact skill version from the platform and installs it into the instance

The only manual actions are:

1. Upload the local archive
2. Attach the uploaded skill to the target instance

## Current boundary

This manual path being available does not mean the following are back in default release scope:

- `skill-scanner` default enablement
- FastSkill auto-discovery
- frozen U3 validation
- automatic population of `/api/v1/admin/skills`
