[← README로 돌아가기](../README.ko.md)

# ClawManager 사용자 매뉴얼

현재 제품 UI의 일반 사용자와 관리자 작업을 한곳에 정리한 기본 설명서입니다. 긴 전문 절차만 별도 가이드로 유지합니다.

## 목차

- [1. 배포와 로그인](#deploy-and-sign-in)
- [2. 역할과 탐색](#roles-and-navigation)
- [3. 모델 구성](#configure-model-access)
- [4. 워크스페이스 생성](#create-a-workspace)
- [5. 인스턴스 운영](#operate-an-instance)
- [6. 리소스와 Skill](#resources-and-skills)
- [7. Team 협업](#team-collaboration)
- [8. 관리자 작업](#administration)
- [9. Runtime 이미지와 Lite 롤링 업데이트](#runtime-images-and-rollout)
- [10. AI Gateway와 Session Usage](#ai-gateway)
- [11. Security Protection](#security-protection)
- [12. Clipboard와 Desktop](#clipboard-and-desktop)
- [13. 문제 해결과 검수](#troubleshooting)
- [14. 전문 가이드](#focused-guides)

<a id="deploy-and-sign-in"></a>
## 1. 배포와 로그인

k3s/Kubernetes 각각 Single-Node HostPath와 Multi-Node CSI/RWX, 총 네 가지 profile을 제공합니다. 하나의 완전한 profile만 적용하고 manifest를 섞거나 임시 HostPath로 Multi-Node storage를 보완하지 마세요. Workload와 PVC가 준비되면 설정된 주소에서 관리자가 만든 계정으로 로그인합니다. ARM64 확인을 포함한 세부 내용은 [Deployment Guide](./deployment_ko.md)를 참고하세요.

<a id="roles-and-navigation"></a>
## 2. 역할과 탐색

사용자 영역은 Workbench, My Instances, Teams, Resource Management, Skill Hub, Settings로 구성됩니다. Resource Management는 시작 리소스를 준비하고, Skill Hub는 모든 지원 Runtime이 함께 사용하는 Version 관리 Skill Catalog입니다. 관리자 영역에는 Users, 전체 Instances, Runtime Pool, Security Protection, AI Gateway, System Settings가 추가됩니다. 보이는 작업은 Role과 Quota에 따라 달라집니다.

<a id="configure-model-access"></a>
## 3. 모델 구성

**AI Gateway → Models**에서 일반 모델을 하나 이상 추가·활성화하고 Health를 테스트합니다. Security Model은 Risk Rule이 민감 요청을 별도 경로로 보낼 때만 필요합니다. Managed Thinking은 제어 가능한 Provider/Model의 영구 설정이며 지연과 Reasoning Token을 늘릴 수 있지만 비공개 사고 내용을 표시하지 않습니다.

<a id="create-a-workspace"></a>
## 4. 워크스페이스 생성

**My Instances → Create**에서 Runtime과 Mode를 선택합니다.

| Runtime | 주요 용도 | Lite | Pro |
|---|---|---|---|
| OpenClaw | 대화, 도구, 예약 작업, Team Leader/Worker | 공유 Pool | 전용 Desktop |
| Hermes | Hermes 기본 Session/Tool, Team Worker | 공유 Pool | 전용 Desktop |
| OpenCode | AI Gateway, File, Terminal/Desktop Coding Workspace | 공유 Pool | 전용 Desktop |
| DeepSeek Harness | AI Gateway, Skill, Workspace File, Native Browser UI를 제공하는 관리형 Agent Workspace | 공유 Pool | 전용 Webtop |

선택에 따라 System Image, Resource Preset 또는 CPU/Memory/Storage, Stream Profile, Environment, Archive, Resource Pack, 개별 Resource, 초기 Skill이 표시됩니다. Lite는 인스턴스별 Pod를 만들지 않고 공유 Runtime Pod 안에서 격리 Workspace/Process를 실행합니다.

Skill Hub는 OpenCode 전용이 아닙니다. OpenClaw, Hermes, OpenCode, DeepSeek Harness가 같은 Catalog를 사용하며 저장 경로와 Reload 방법만 Runtime마다 다릅니다. 생성 화면에 Skill 선택이 없다면 인스턴스 준비 후 Skill Hub 또는 인스턴스 화면에서 설치하세요.

<a id="operate-an-instance"></a>
## 5. 인스턴스 운영

- Start / Stop / Restart는 ClawManager에서 실행하고 생성된 Kubernetes Object를 직접 수정하지 않습니다. Environment Override는 지정된 Restart로 적용합니다.
- Delete 전에 필요한 File/Archive를 보관합니다.
- Share Link는 Credential, 만료, Workspace 접근 범위를 설정하고 필요 없으면 폐기합니다.
- Runtime/Storage가 지원하면 Workspace File을 조회, Upload, Download, Edit, Delete할 수 있습니다.
- Low / Standard / High는 대역폭과 화질을 조정하며 저장 후 보통 Restart/Apply가 필요합니다.
- Skill Management에서 실제 Version을 확인하고 Session Usage에서는 Runtime이 보고한 데이터만 확인합니다.
- Dedicated Instance는 진단용 Runtime Overview / Events를 제공할 수 있습니다.

<a id="resources-and-skills"></a>
## 6. 리소스와 Skill

Resource Management에는 **Resources**, **Resource Packs**, 읽기 전용 **Injection Records**가 있습니다. Resources는 Channel, 업로드 Skill, Scheduled Task를 관리하며 Agent type은 현재 예약 유형입니다.

Skill Hub는 Runtime 공통 Skill 관리·배포 플랫폼입니다. Browse, My Skills, Ownership, Tag, Version, Scan, Publish, Install, 인스턴스 확인을 담당합니다. ZIP은 `SKILL.md`를 포함해야 하며 Scan Failure는 수정할 수 있도록 남습니다. Scan 완료가 자동 승인이라는 뜻은 아닙니다. OpenClaw, Hermes, OpenCode, DeepSeek Harness가 모두 대상입니다. [Resource Management](./resource-management_ko.md)와 [Skill Hub](./skill-hub-guide_ko.md)를 참고하세요.

<a id="team-collaboration"></a>
## 7. Team 협업

**Teams → Create**에서 변경할 수 없는 내장 Template 8개 또는 Custom Template을 선택합니다. Leader는 OpenClaw Lite, Worker는 OpenClaw Lite 또는 Hermes Lite입니다. Custom Team은 2–6명으로 Intent 생성, 이름/Intent/인원 수정, 전체 재생성, 역할별 자연어 조정, 삭제와 재사용을 지원합니다. Leader 조정은 Domain 역할만 확장하며 Delegation, 결과 수집, 최종 보고를 제거하지 않습니다.

Chat, 최신 Query의 Execution Kanban, Files, Artifacts, Member Delivery, Final Result를 함께 확인합니다. 새 질문은 최신 Task Group을 기본 선택합니다. [Team Guide](./team-workspaces-guide_ko.md)를 참고하세요.

<a id="administration"></a>
## 8. 관리자 작업

Users는 계정, Role, Quota, CSV Import를, Instances는 전역 검색과 Lifecycle을, Runtime은 공유 Pod, Capacity, Health, Maintenance Drain을 관리합니다. Settings는 Image와 Lite Rollout을 관리합니다. Security Protection과 AI Gateway는 Resource Management와 분리된 관리자 영역입니다.

<a id="runtime-images-and-rollout"></a>
## 9. Runtime 이미지와 Lite 롤링 업데이트

**Admin Console → Settings**를 엽니다.

![Runtime 이미지 설정과 Lite 롤링 업데이트](./main/runtime-settings-rollout.png)

1. Lite/Pro 카드에 Image를 입력하고 **Save**합니다. 이는 향후 Provisioning 설정만 저장하며 실행 중인 Lite Pod를 교체하지 않습니다.
2. 실행 Pool을 갱신하려면 상단 **Lite Runtime Rolling Upgrade**에서 OpenClaw Lite, Hermes Lite, OpenCode Lite, DeepSeek Harness Lite를 선택하고 Current/Target Image, Batch, Max Unavailable을 확인합니다.
3. **Start Rolling Upgrade**로 Drain과 Replacement를 순차 실행합니다.
4. 완료 후 Runtime Health와 Test Instance를 확인합니다.

Batch가 크면 빠르지만 가용 Capacity가 줄어듭니다. Drain 중 Active Lite Session이 중단될 수 있으므로 보수적인 값과 유지보수 시간을 사용하세요. Pro Image 저장은 기존 Pro Instance를 자동 교체하지 않습니다.

<a id="ai-gateway"></a>
## 10. AI Gateway와 Session Usage

다섯 영역은 Models, AI Audit, Costs, Session Usage, Risk Rules입니다. Session Usage는 관측 화면이지 대화 편집기나 청구 원장이 아닙니다. 기간, User, Runtime, Instance, Session으로 필터하고 보고된 Input/Output/Cached/Reasoning Token을 비교한 뒤 요청 단위 근거는 AI Audit에서 확인합니다. [AI Gateway Guide](./aigateway_ko.md)를 참고하세요.

<a id="security-protection"></a>
## 11. Security Protection

Security Protection은 별도 관리자 영역입니다. Alert Metric, Event, Pod Live Aegis, Export, Emergency Control, KSecure Model과 Runtime Defense, Isolation, Trust, Identity/Egress, Policy, Collaboration, Quota/Approval, Skill Scanner, Audit 세부 화면을 제공합니다. 사용자는 Skill Hub에서 Scan 상태를 보고 관리자는 여기서 Scanner Health와 Security Evidence를 관리합니다. [Security Guide](./security-platform_ko.md)를 참고하세요.

<a id="clipboard-and-desktop"></a>
## 12. Clipboard와 Desktop

Clipboard는 Runtime Image에 따라 양방향, Host→Desktop만, 또는 비활성입니다. 변경에는 보통 Restart가 필요합니다. ASCII 다음 Unicode/CJK를 테스트하세요. Clipboard와 Keyboard/IME는 별도 경로이며 Browser Permission도 영향을 줍니다. Password/API Key로 테스트하지 마세요.

<a id="troubleshooting"></a>
## 13. 문제 해결과 검수

| 증상 | 확인 |
|---|---|
| Runtime/Image가 없음 | Image Save와 Enable 상태. |
| 저장한 Lite Image가 실행되지 않음 | Rolling Upgrade도 시작해야 함. |
| Model이 없음 | 일반 모델 하나 이상 활성화. |
| Lite 전용 Pod가 없음 | 정상: 공유 Runtime Pod 사용. |
| PVC Pending | Profile, StorageClass, AccessMode, Node Label, Capacity. |
| Skill이 보이지 않음 | Version/Path, Refresh, 필요한 Runtime Reload. |
| Session Usage가 비어 있음 | 기간/Filter와 Runtime 보고 여부. |

검수 시 Workload/PVC, 일반 Model, 공개 Runtime별 Test Instance, Lifecycle/File, Skill Install, AI Audit/Session Usage, Team 사용 시 Chat/Kanban/File/Result를 확인합니다.

<a id="focused-guides"></a>
## 14. 전문 가이드

- [Deployment](./deployment_ko.md)
- [Team](./team-workspaces-guide_ko.md)
- [AI Gateway](./aigateway_ko.md)
- [Security Protection](./security-platform_ko.md)
- [Resource Management](./resource-management_ko.md)
- [Skill Hub](./skill-hub-guide_ko.md)
- [OpenCode Workspace](./opencode-lite-pro-agent-development_ko.md)
