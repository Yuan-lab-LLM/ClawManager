[← README로 돌아가기](../README.ko.md)

# Skill Hub 사용자 가이드

Skill Hub는 OpenClaw, Hermes, OpenCode, DeepSeek Harness가 함께 사용하는 Version 관리 Skill Catalog입니다. 인스턴스 파일을 Scan, Publish, 재설치 가능한 Asset으로 바꾸는 플랫폼이며 OpenCode 전용 기능이 아닙니다.

## 화면

- **Browse**: Published Skill 검색, Tag Filter, Author/Version/Scan/Risk 확인, 호환 Instance 설치.
- **My Skills**: Upload 또는 Instance에서 수집한 Skill, Version, Tag, Publish/Unpublish, Download, Delete 관리.
- **Admin**: Platform Skill과 허용된 Governance. Button은 Ownership과 Version State에 따라 달라집니다.
- **Instance Skill Management**: Installed, Hub-managed, Workspace-discovered Skill과 실제 Version 확인.

## Upload, Scan, Publish

1. My Skills에서 `SKILL.md`와 필요한 File을 포함한 ZIP을 하나 이상 Upload합니다.
2. ClawManager가 Version을 저장하고 Security Scan을 시작합니다. 일반적인 Windows/CJK ZIP Filename을 처리합니다.
3. Scan Status, Risk, Finding을 확인합니다. Failed Version은 수정할 수 있도록 남습니다.
4. Version과 Policy 조건을 충족하면 Public Tag를 설정하고 Publish합니다.
5. Unpublish는 Browse에서만 제거하며 Owner History는 삭제하지 않습니다.

Scan Completed가 자동 안전 보장이나 승인이라는 뜻은 아닙니다. Package, Ownership, Runtime Compatibility, Platform Policy도 적용됩니다.

## Install과 확인

Skill Detail에서 Install을 선택하고 호환 Instance와 Version을 확정합니다. 이후 Instance Skill Management를 Refresh하여 실제 Version을 확인합니다. OpenClaw, Hermes, OpenCode, DeepSeek Harness를 지원하지만 저장 경로와 Reload 방식은 Runtime마다 다릅니다. DeepSeek Harness는 Lite에서 `home/.dsh/skills`, Pro에서 `.dsh/skills`를 사용합니다.

## Instance에서 수집

Workspace Discovery는 자동으로 My Skills에 추가하지 않습니다. 내용과 Source를 확인하고 **Collect to library**를 실행한 뒤 Package/Scan 완료 후 Publish를 결정합니다. Drift가 있으면 Installed Version을 복원하거나 현재 상태를 New Version으로 수집하고 History를 조용히 덮어쓰지 않습니다.

## 경계와 문제 해결

- 기존 YAML Frontmatter는 다시 쓰지 않으므로 `name`과 `description`을 확인합니다.
- Capability Tag는 검색용이며 자동 Install이 아닙니다.
- Publish 비활성: Scan, Package, Ownership, Risk/Policy 확인.
- Target Instance 없음: Ownership, Runtime Support, Instance State 확인.
- Install 후 안 보임: Skill Management, Version/Path, 필요한 Runtime Reload 확인.

[Resource Management](./resource-management_ko.md), [Security Protection](./security-platform_ko.md), [사용자 매뉴얼](./use_guide_ko.md)도 참고하세요.
