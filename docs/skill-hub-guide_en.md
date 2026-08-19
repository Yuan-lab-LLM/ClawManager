[← Back to README](../README.md)

# Skill Hub User Guide

Skill Hub is ClawManager's reusable skill catalog for OpenClaw, Hermes, OpenCode, and DeepSeek Harness workspaces. It turns files from one instance into scanned, versioned, publishable assets that can be installed again.

## Views

- **Browse**: search published skills, filter by tag, inspect version/author/scan/risk information, and install to compatible instances.
- **My Skills**: manage skills you uploaded or collected from an instance, including versions, tags, publication, unpublishing, download, and deletion.
- **Admin**: review platform skills and perform permitted governance actions. Visible buttons still depend on ownership and version state.
- **Instance Skill Management**: inspect installed, Hub-managed, and workspace-discovered skills for one instance.

## Upload and Publish

1. Upload one or more ZIP files under **My Skills**. Each package should contain `SKILL.md` and its supporting files.
2. ClawManager stores a version and starts security scanning. Common Windows/Chinese ZIP filename encodings are handled.
3. Review scan status, risk, and findings. A failed scan keeps the uploaded version visible so it can be corrected.
4. When the current version satisfies publication requirements, assign public tags and publish it.
5. Published versions appear in Browse; unpublishing does not erase the owner's history.

A completed scan is not a guarantee of zero risk. Publication and installation also depend on the package, ownership, runtime compatibility, and current platform policy.

## Install to an Instance

1. Open a skill and choose **Install**.
2. Select one or more compatible instances.
3. Confirm the version.
4. Open each instance and refresh **Skill Management** to verify the effective version.

OpenClaw, Hermes, OpenCode, and DeepSeek Harness are supported, but their materialization paths and reload behavior differ. DeepSeek Harness uses `home/.dsh/skills` in Lite workspaces and `.dsh/skills` in Pro workspaces. OpenCode skill preselection may not be shown during instance creation; install after creation from Skill Hub or the instance page. OpenCode Pro on non-HostPath storage additionally depends on the Runtime Agent's skill-command support.

## Collect a Skill from an Instance

Workspace discovery does not automatically add a skill to My Skills. Review the files, choose **Collect to library**, let ClawManager package and scan the version, and publish only after review.

When workspace files differ from the installed version, the instance panel reports drift. Restore the installed version or collect the current files as a new version instead of silently overwriting history.

## Boundaries

- Existing YAML frontmatter in `SKILL.md` is not rewritten; validate `name` and `description` before upload.
- Capability tags improve description and search; they do not install a skill automatically.
- Risk is decision evidence. Available actions may differ by UI path and organization policy.
- Backend administrator permissions and buttons currently exposed by the Admin view are not identical for every ownership case.

## Troubleshooting

| Symptom | What to do |
|---|---|
| Scan failed | Read the error, correct the ZIP, and upload a new version. |
| Publish is disabled | Check scan state, package availability, ownership, and risk/policy restrictions. |
| Target instance is missing | Confirm ownership, supported runtime, and operable instance state. |
| Installed skill is not visible | Refresh instance Skill Management, verify the materialized version, and reload the runtime if required. |

## Related Guides

- [Resource Management Guide](./resource-management.md)
- [Security Protection Platform Guide](./security-platform.md)
- [User Guide](./use_guide_en.md)
