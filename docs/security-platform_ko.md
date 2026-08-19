[← README로 돌아가기](../README.ko.md)

# Security Protection Platform 가이드

**Security Protection**은 관리자용 독립 작업 공간이며 사용자 리소스 관리에 포함되지 않습니다. Runtime 방어, Host/Container 보호, Component Trust, Identity, Policy, Collaboration Governance, 비상 대응, Security Event를 한 화면에 모읍니다.

![ClawManager Security Protection](./main/security-protection-current.png)

## Overview와 작업

네 개의 지표는 오늘의 방어 Hit, 최근 24시간 High Severity, Block/Deny Event, 영향을 받은 Agent Instance를 보여 줍니다. Alert는 자동 갱신되고 최근 10개 Event에는 시간, Source, Scenario, Evidence, Target, Severity가 표시됩니다.

- **Pod Live Aegis Configuration**: Runtime Security 구성과 Dispatch 흐름을 엽니다.
- **Report Export**: 현재 로드된 Alert를 JSON Lines로 저장합니다.
- **Emergency Circuit Breaker**: 사유와 확인을 받은 후 비상 상태를 전달하며, 활성화 중에는 실행자, 시각, 사유와 해제 작업을 표시합니다.

Live 구성이나 Circuit Breaker를 실행하기 전에 대상 범위와 영향을 확인하세요.

## KSecure 모델

UI는 **7 Risk Surface, 15 Defense Scenario, 4 Defense Layer**로 모델을 표현하며 Layer View와 Ring View를 제공합니다.

- **Runtime Layer**: Input, State/Memory, Decision/Tool Call, Output, Asset Protection, Human Approval.
- **Host Layer**: Host Hardening과 Container Isolation.
- **Audit Layer**: Skill Scanner와 통제된 Private Egress Exception.
- **Control Layer**: Outbound/Identity Governance, Policy Template, Circuit Breaker, Full-chain Audit, Team Collaboration, AI Gateway Quota.

각 카드는 Scenario 페이지로 연결됩니다. 카드가 보인다고 모든 Backend Enforcement가 활성화된 것은 아니며, 실제 작업은 배포된 Security Service와 Runtime Agent에 따라 달라집니다.

## 운영 흐름과 경계

Event와 영향을 받은 대상을 확인하고 해당 Scenario의 구성 또는 Evidence를 살핀 뒤 영향이 가장 작은 조치를 적용하세요. Circuit Breaker는 중단이 필요하고 범위를 이해한 경우에만 사용합니다.

Resource Management는 Channel, Skill, Scheduled Task, Resource Pack, Injection Record를 관리합니다. Skill Scanner는 Security Protection 내부의 한 Scenario입니다. 사용자는 Skill Hub에서 Upload, Scan Status, Report를 확인하고 관리자는 여기서 Scanner Health, Failed Job, Model/Meta LLM, Quick/Deep Policy, Security Event를 점검합니다. Scan 완료는 자동 승인이 아닙니다. 이 기능은 Kubernetes Hardening, Network Policy, Credential, Backup, 조직 Incident Response를 대체하지 않습니다.

[Skill Hub](./skill-hub-guide_ko.md), [리소스 관리](./resource-management_ko.md), [AI Gateway](./aigateway_ko.md), [사용자 매뉴얼](./use_guide_ko.md)도 참고하세요.
