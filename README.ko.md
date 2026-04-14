# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  ClawManager는 AI Agent 인스턴스 관리를 위한 Kubernetes 네이티브 컨트롤 플레인으로, 거버넌스가 적용된 AI 접근, 런타임 오케스트레이션, 그리고 여러 Agent Runtime 전반에 걸친 재사용 가능한 리소스 관리를 제공합니다.
</p>

<p align="center">
  <strong>언어:</strong>
  <a href="./README.md">English</a> |
  <a href="./README.zh-CN.md">简体中文</a> |
  <a href="./README.ja.md">日本語</a> |
  한국어 |
  <a href="./README.de.md">Deutsch</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ClawManager-Control%20Plane-e25544?style=for-the-badge" alt="ClawManager Control Plane" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
</p>

<p align="center">
  <a href="#product-tour">제품 소개</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#resource-management">리소스 관리</a> |
  <a href="#get-started">시작하기</a>
</p>

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
    <img src="https://img.shields.io/github/stars/Yuan-lab-LLM/ClawManager?style=for-the-badge&logo=github&label=Star%20ClawManager" alt="Star ClawManager on GitHub" />
  </a>
</p>

<h2 align="center">60초 안에 보는 ClawManager</h2>

<p align="center">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-launch-60s-hd.gif" alt="ClawManager 제품 데모" width="100%" />
</p>

<p align="center">
  빠른 Agent 프로비저닝, Skill 관리와 스캔, AI Gateway 거버넌스를 짧게 확인할 수 있습니다.
</p>

## 최신 업데이트

최근의 중요한 제품 및 문서 업데이트입니다.

- [2026-04-08] 플랫폼에 Skill 관리와 Skill 스캔 워크플로우가 추가되었습니다. 자세한 내용은 [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52)를 참고하세요.
- [2026-03-26] AI Gateway 문서를 업데이트하여 모델 거버넌스, 감사와 추적, 비용 계산, 리스크 제어 설명을 강화했습니다. 자세한 내용은 [AI Gateway Guide](./docs/aigateway.md)를 참고하세요.
- [2026-03-20] ClawManager는 AI Agent 워크스페이스를 위한 더 넓은 컨트롤 플레인으로 발전했으며, 런타임 제어, 재사용 가능한 리소스, 보안 스캔 워크플로우가 강화되었습니다.

> ClawManager가 여러분의 팀에 도움이 된다면, 프로젝트에 Star를 남겨 더 많은 사용자와 개발자가 발견할 수 있도록 도와주세요.

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>

<a id="product-tour"></a>
## 제품 소개

ClawManager는 AI Agent 인스턴스 운영을 Kubernetes 위로 확장하고, 그 런타임 기반 위에 3개의 상위 컨트롤 플레인을 제공합니다. 팀은 이를 통해 AI 접근을 통제하고, Agent를 통해 런타임 동작을 오케스트레이션하며, 스캔 가능하고 재사용 가능한 channel 및 skill 리소스로 워크스페이스 기능을 제공할 수 있습니다.

다음과 같은 팀에 적합합니다.

- 여러 사용자를 대상으로 AI Agent 인스턴스를 운영하는 플랫폼 팀
- 런타임 가시성, 명령 배포, desired state 제어가 필요한 운영 팀
- 수동 설정 대신 재사용 가능한 리소스로 Agent 워크스페이스를 제공하고 싶은 개발 팀

<a id="get-started"></a>
## 시작하기

ClawManager는 이제 표준 Kubernetes 환경과 경량 클러스터 환경 모두에 대해 더 명확한 진입 경로를 제공합니다. 먼저 자신의 환경에 맞는 배포 경로를 선택한 뒤, 첫 로그인 및 기본 사용 흐름으로 이어가면 됩니다.

- 표준 Kubernetes 배포: [deployments/k8s/clawmanager.yaml](./deployments/k8s/clawmanager.yaml)
- K3s / 경량 클러스터 배포: [deployments/k3s/clawmanager.yaml](./deployments/k3s/clawmanager.yaml)
- 첫 로그인 및 기본 사용 흐름: [사용자 가이드](./docs/use_guide_ko.md)
- 배포 설명 및 아키텍처 배경: [Deployment Guide (English)](./docs/deployment.md)

## 세 가지 컨트롤 플레인

<a id="ai-gateway"></a>
### AI Gateway

AI Gateway는 ClawManager에서 모델 접근을 거버넌스하는 컨트롤 플레인입니다. 관리되는 Agent Runtime에 통합된 OpenAI 호환 진입점을 제공하고, 상위 모델 제공자 위에 정책, 감사, 비용 제어를 추가합니다.

- 모델 트래픽을 위한 통합 진입점
- 보안 모델 라우팅과 정책 기반 모델 선택
- 엔드투엔드 감사 및 추적 기록
- 내장된 비용 계산과 사용량 분석
- 차단 또는 라우팅 전환이 가능한 리스크 제어 규칙

[AI Gateway Guide (English)](./docs/aigateway.md)를 참고하세요.

<a id="agent-control-plane"></a>
### Agent Control Plane

Agent Control Plane은 관리되는 AI Agent 인스턴스를 위한 런타임 오케스트레이션 계층입니다. 각 인스턴스를 등록, 상태 보고, 명령 수신, 그리고 플랫폼 측 desired state와의 정렬이 가능한 관리형 런타임으로 만듭니다.

- 보안 부트스트랩과 세션 라이프사이클 기반 Agent 등록
- 하트비트 기반 런타임 상태 및 헬스 리포팅
- 컨트롤 플레인과 인스턴스 간 desired state 동기화
- 시작, 중지, 설정 적용, 헬스체크, Skill 작업을 위한 명령 배포
- 인스턴스 단위의 Agent 상태, channel, skill, 명령 이력 가시화

[Agent Control Plane Guide (English)](./docs/agent-control-plane.md)를 참고하세요.

<a id="resource-management"></a>
### 리소스 관리

리소스 관리는 AI Agent 워크스페이스를 위한 재사용 가능한 자산 계층입니다. 팀은 channel과 skill을 준비하고, bundle로 조합하고, 인스턴스에 주입하며, 그 과정에 보안 검토를 자연스럽게 포함시킬 수 있습니다.

- `Channel` 관리: 워크스페이스 연결과 통합 템플릿
- `Skill` 관리: 재사용 가능한 기능 패키지
- `Skill Scanner` 워크플로우: 리스크 검토와 스캔 작업
- bundle 기반 리소스 조합: 반복 가능한 워크스페이스 구성
- 주입 스냅샷을 통한 실제 적용 결과 추적

[Resource Management Guide (English)](./docs/resource-management.md)와 [Security / Skill Scanner Guide (English)](./docs/security-skill-scanner.md)를 참고하세요.

## 제품 갤러리

ClawManager는 관리, 접근, AI 거버넌스를 서로 분리된 도구로 다루지 않고, 하나의 일관된 제품 경험으로 묶도록 설계되었습니다.

### 관리 콘솔

관리 콘솔은 사용자, 쿼터, 런타임 작업, 보안 제어, 플랫폼 수준 정책을 하나의 화면으로 묶습니다. 대규모 AI Agent 인프라를 운영하는 팀의 핵심 작업 공간입니다.

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager 관리 콘솔" width="100%" />
</p>

### Portal Access

Portal은 사용자에게 일관된 워크스페이스 진입점을 제공합니다. 브라우저 기반으로 접근하면서도 컨트롤 플레인과 동기화된 런타임 상태를 확인할 수 있어, 사용자가 인프라 세부 사항을 직접 다루지 않아도 됩니다.

<p align="center">
  <img src="./docs/main/portal.png" alt="ClawManager Portal Access" width="100%" />
</p>

### AI Gateway

AI Gateway는 모델 사용 거버넌스를 워크스페이스 경험 자체에 통합합니다. 감사 로그, 비용 가시성, 리스크 라우팅을 제공하여 AI 사용을 개별 통합이 아닌 플랫폼 기능으로 다룰 수 있게 합니다.

<p align="center">
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="100%" />
</p>

## 동작 방식

1. 관리자가 거버넌스 정책과 재사용 가능한 리소스를 정의합니다.
2. 사용자가 Kubernetes에서 관리되는 AI Agent 워크스페이스를 생성하거나 진입합니다.
3. Agent가 컨트롤 플레인에 연결해 런타임 상태를 보고합니다.
4. Channel, skill, bundle이 컴파일되어 인스턴스에 적용됩니다.
5. AI 트래픽은 AI Gateway를 통해 전달되며, 감사, 리스크, 비용 제어가 함께 적용됩니다.

## 개발자 개요

ClawManager는 React 프런트엔드, Go 백엔드, 상태 저장용 MySQL, 그리고 `skill-scanner` 및 오브젝트 스토리지 통합을 포함한 Kubernetes 네이티브 플랫폼입니다. 코드베이스는 제품 서브시스템 단위로 구성되어 있으므로, 관련 가이드에서 시작한 뒤 코드로 들어가는 방식이 가장 효율적입니다.

- 프런트엔드의 관리자 및 사용자 화면은 `frontend/`
- 백엔드 서비스, handler, repository, migration은 `backend/`
- 배포 자산은 `deployments/`
- 제품 문서와 이미지 자산은 `docs/`

[Developer Guide (English)](./docs/developer-guide.md)를 참고하세요.

## 문서

- [사용자 가이드](./docs/use_guide_ko.md)
- [Deployment Guide (English)](./docs/deployment.md)
- [Admin and User Guide (English)](./docs/admin-user-guide.md)
- [Agent Control Plane Guide (English)](./docs/agent-control-plane.md)
- [AI Gateway Guide (English)](./docs/aigateway.md)
- [Security / Skill Scanner Guide (English)](./docs/security-skill-scanner.md)
- [Resource Management Guide (English)](./docs/resource-management.md)
- [Developer Guide (English)](./docs/developer-guide.md)

## 라이선스

이 프로젝트는 MIT License로 공개됩니다.

## 오픈소스

Issue와 Pull Request를 환영합니다.

## Star History

<a href="https://www.star-history.com/?repos=Yuan-lab-LLM%2FClawManager&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
 </picture>
</a>
