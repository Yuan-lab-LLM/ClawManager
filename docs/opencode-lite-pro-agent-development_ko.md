[← README로 돌아가기](../README.ko.md)

# OpenCode 워크스페이스 가이드

OpenCode는 ClawManager의 관리형 Coding Workspace입니다. 공식 OpenCode를 사용하고 AI Gateway를 통해 Model에 접근합니다.

## Lite와 Pro

| Mode | 형태 | 경계 |
|---|---|---|
| Lite | 공유 Runtime Pod 내부의 격리 Process/Workspace | Instance별 Pod 없음 |
| Pro | Dedicated Desktop Workload | Default Image Save가 기존 Instance를 자동 교체하지 않음 |

두 Mode 모두 선택한 Storage Profile에 따라 Workspace를 영구 저장합니다. Lite Portal은 ClawManager가 적응하고 Pro는 Dedicated Desktop에서 OpenCode를 엽니다.

## 생성 전

관리자는 호환 OpenCode Image와 일반 AI Gateway Model을 하나 이상 활성화해야 합니다. Lite Pool은 Healthy 상태여야 하며 New Image Save 후에는 Lite Rolling Upgrade도 필요합니다. User Resource Quota도 확인하세요.

OpenCode는 관리형 AI Gateway Provider 구성을 받습니다. 관리자가 설계하지 않았다면 OpenCode에서 별도 Provider Key를 추가하지 마세요.

## 사용

**My Instances → Create**에서 OpenCode와 Lite/Pro를 선택하고 Image, Resource, Environment, 표시된 Start Resource를 설정합니다. Instance Page에서 Lifecycle, Terminal/Desktop, Files를 사용합니다.

- Start/Stop/Restart/Delete는 ClawManager에서 실행합니다.
- Project는 임시 Directory가 아니라 표시된 Workspace에 저장합니다.
- Storage 지원 범위에서 File Panel Upload/Download/Edit/Delete를 사용합니다.
- Stream Profile과 Environment 변경은 보통 Apply/Restart가 필요합니다.
- Share Link에는 만료, Credential, 최소 Workspace Permission을 설정합니다.

## AI Gateway와 Skill

Model Error는 Instance State, Model Health, Protocol, AI Audit 순서로 확인합니다. 일반 사용에는 Security Model이 필요하지 않습니다.

Skill Hub는 OpenClaw, Hermes, OpenCode, DeepSeek Harness 공통 기능입니다. OpenCode Lite는 `{workspace}/home/.opencode/skills`, managed HostPath Pro는 `/config/workspace/.opencode/skills`를 사용합니다. Creation에 선택이 없으면 이후 Install하고 Skill Management에서 확인합니다. Non-HostPath Pro는 Runtime Agent Command가 필요합니다.

## 경계와 문제 해결

- OpenClaw Config Plan, Archive, Team Persona를 상속하지 않습니다.
- Standard Team은 현재 OpenCode를 Leader/Worker로 사용하지 않습니다.
- Scheduled Task는 UI가 표시할 때만 사용 가능하다고 판단합니다.
- Old Lite Image: Save 후 Rolling Upgrade.
- Portal Failure: Instance/Pool Health와 Event.
- File Loss: Workspace Path, PVC, Storage Profile.
- Skill Failure: Materialization과 Runtime Agent Capability.

Create/Start/Stop/Restart, Portal/Desktop, Streaming/Tool, Persistence, Share Link, 사용하는 Skill Flow를 검수합니다. [사용자 매뉴얼](./use_guide_ko.md), [AI Gateway](./aigateway_ko.md), [Skill Hub](./skill-hub-guide_ko.md)도 참고하세요.
