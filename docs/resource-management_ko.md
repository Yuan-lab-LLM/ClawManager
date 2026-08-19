[← README로 돌아가기](../README.ko.md)

# 리소스 관리 가이드

리소스 관리는 재사용 가능한 OpenClaw 시작 구성을 준비하는 사용자 기능입니다. 관리자용 **Security Protection**과는 별개이며, 리소스 관리는 구성과 전달을, Security Protection은 위험 관찰과 거버넌스를 담당합니다.

![OpenClaw 리소스 관리](./main/resource-management-current.png)

## 세 개의 탭

- **리소스**: 개별 정의 검색 및 관리.
- **리소스 팩**: 여러 리소스를 반복 사용 가능한 시작 구성으로 결합.
- **주입 기록**: 인스턴스 생성 시 컴파일되고 재시작 시 재사용되는 스냅샷 확인.

## 리소스 유형

- **Channel**: 통신 구성을 생성, 편집, 활성화/비활성화, 복제, 삭제합니다. Telegram, DingTalk, WeCom, Slack, Feishu 등 지원 템플릿은 Form과 JSON 편집을 제공합니다.
- **Skill**: 하나 이상의 ZIP을 업로드하고 충돌을 해결하며 다운로드하거나 삭제합니다. Catalog, Ownership, Version, Publish, 이후 Install은 **Skill Hub**에서 관리합니다.
- **Agent**: 예약된 유형으로 보이지만 현재 이 화면에서는 구성할 수 없습니다.
- **Scheduled Task**: 간단한 Form 또는 고급 JSON으로 OpenClaw Job을 만들고 편집합니다. cron, 간격, 일회 실행과 announce, webhook, 전달 없음 모드를 지원합니다.

Session Template과 Log Policy는 내부 모델에 있지만 이 화면에서는 의도적으로 숨겨져 있습니다.

## 리소스 팩과 주입 기록

리소스 팩은 활성화된 리소스와 대상 Skill을 묶으며 생성, 편집, 활성화/비활성화, 복제, 삭제할 수 있습니다. 여러 인스턴스에 같은 기준 구성을 전달할 때 사용합니다.

주입 기록은 읽기 전용이며 Snapshot ID, 전달 모드, 리소스 수, 환경 변수 수, 상태, 생성 시각을 보여 줍니다. 실제 전달 내용을 확인하는 기록이지 보안 이벤트는 아닙니다.

## 다른 기능과의 경계

- **Skill Hub**는 OpenClaw, Hermes, OpenCode, DeepSeek Harness용 Skill Catalog, Version, Publish, Install을 관리합니다.
- **인스턴스 생성**에서는 Runtime 지원 범위에 따라 Archive, Resource Pack, 개별 Resource, Skill을 선택합니다.
- **Security Protection**은 Runtime 방어, 격리, 정책, 비상 대응, Audit을 위한 별도의 관리자 기능입니다. Skill Scanner는 그 안의 한 Scenario이며 리소스 관리 탭이 아닙니다.

[Skill Hub](./skill-hub-guide_ko.md), [Security Protection](./security-platform_ko.md), [사용자 가이드](./use_guide_ko.md)도 참고하세요.
