# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  팀 규모부터 클러스터 규모까지 OpenClaw와 Linux 데스크톱 런타임을 통합 관리하기 위한 Kubernetes-first 제어 평면입니다.
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
  <img src="https://img.shields.io/badge/ClawManager-Virtual%20Desktop%20Platform-e25544?style=for-the-badge" alt="ClawManager Platform" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
</p>

## News

- [2026-03-26]: 🚀🚀 AI Gateway 문서와 개요를 업데이트했습니다. 모델 거버넌스, 감사 추적, 비용 정산, 위험 제어를 정리했습니다. 자세한 내용은 [AI Gateway](#ai-gateway)를 참고하세요.
- [2026-03-20]: 🎉🎉 ClawManager 릴리스 —— ClawManager는 현재 가상 데스크톱 관리 플랫폼으로 제공되며, 일괄 배포, Webtop 지원, 데스크톱 포털 접근, 런타임 이미지 설정, OpenClaw 메모리/환경설정 Markdown 백업 및 마이그레이션, 클러스터 리소스 개요, 다국어 문서를 지원합니다.

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager Admin" width="32%" />
  <img src="./docs/main/portal.png" alt="ClawManager Portal" width="32%" />
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="32%" />
</p>

## 무엇인가

ClawManager는 Kubernetes 위에서 데스크톱 런타임의 배포, 운영, 접근을 한곳에서 관리할 수 있게 해줍니다.

다음과 같은 환경에 적합합니다.

- 여러 사용자를 위한 데스크톱 인스턴스를 만들어야 하는 경우
- quota, 이미지, 라이프사이클을 중앙에서 관리해야 하는 경우
- 데스크톱 서비스를 클러스터 내부에 유지하고 싶은 경우
- Pod를 직접 노출하지 않고 안전한 브라우저 접근을 제공하고 싶은 경우

## 선택하는 이유

- 사용자, quota, 인스턴스, 런타임 이미지를 하나의 관리 화면에서 운영
- OpenClaw 메모리와 설정의 가져오기/내보내기 지원
- 서비스를 직접 노출하지 않고 플랫폼을 통한 안전한 데스크톱 접근
- AI Gateway를 통한 통제된 모델 접근, 감사 추적, 비용 분석, 위험 제어
- Kubernetes에 자연스럽게 맞는 배포 및 운영 흐름
- 관리자 주도 배포와 셀프서비스 생성 모두 지원

## 빠른 시작

### 사전 조건

- 사용 가능한 Kubernetes 클러스터
- `kubectl get nodes` 가 정상 동작해야 함

### 배포

저장소에 포함된 매니페스트를 그대로 적용합니다.

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## 소스 코드에서 빌드

저장소에 포함된 Kubernetes 매니페스트 대신 소스 코드에서 ClawManager를 실행하거나 패키징하려면:

### 프런트엔드

```bash
cd frontend
npm install
npm run build
```

### 백엔드

```bash
cd backend
go mod tidy
go build -o bin/clawreef cmd/server/main.go
```

### Docker 이미지

저장소 루트에서 전체 애플리케이션 이미지를 빌드합니다.

```bash
docker build -t clawmanager:latest .
```

### 기본 계정

- 기본 관리자 계정: `admin / admin123`
- 가져온 관리자 사용자의 기본 비밀번호: `admin123`
- 가져온 일반 사용자의 기본 비밀번호: `user123`

### 첫 사용 순서

1. 관리자 계정으로 로그인합니다.
2. 사용자를 생성하거나 가져오고 quota를 할당합니다.
3. 시스템 설정에서 런타임 이미지 카드를 검토하거나 업데이트합니다.
4. 일반 사용자로 로그인해 인스턴스를 생성합니다.
5. Portal View 또는 Desktop Access를 통해 데스크톱에 접근합니다.

## 주요 기능

- 인스턴스 라이프사이클 관리: 생성, 시작, 중지, 재시작, 삭제, 조회, 동기화
- 지원 런타임: `openclaw`, `webtop`, `ubuntu`, `debian`, `centos`, `custom`
- 관리자 화면에서의 런타임 이미지 카드 관리
- CPU, 메모리, 스토리지, GPU, 인스턴스 수에 대한 사용자 단위 quota 제어
- 노드, CPU, 메모리, 스토리지를 위한 클러스터 리소스 개요
- 토큰 기반 데스크톱 접근과 WebSocket 포워딩
- AI Gateway를 통한 모델 관리, 추적 가능한 감사 로그, 비용 정산, 위험 제어
- CSV 기반 대량 사용자 가져오기
- 다국어 인터페이스

## AI Gateway

AI Gateway는 ClawManager에서 모델 접근을 다루는 거버넌스 평면입니다. OpenClaw 인스턴스에 단일 OpenAI 호환 진입점을 제공하고, 상위 Provider 위에 정책, 감사, 비용 제어를 추가합니다.

- 일반 모델과 보안 모델 관리, Provider 연결, 활성화, 엔드포인트 설정, 가격 정책
- 요청, 응답, 라우팅 결정, 위험 히트를 포함한 엔드 투 엔드 감사 및 추적 기록
- 토큰 집계와 사용량 추정을 포함한 내장 비용 정산
- 설정 가능한 규칙 기반 위험 제어와 `block`, `route_secure_model` 같은 자동 동작

스크린샷, 전체 기능 설명, 모델 선택 및 라우팅 흐름은 [docs/aigateway.md](./docs/aigateway.md)를 참고하세요.

## 사용 흐름

1. 관리자가 사용자, quota, 런타임 이미지 정책을 정의합니다.
2. 사용자가 OpenClaw 또는 Linux 데스크톱 인스턴스를 생성합니다.
3. ClawManager가 Kubernetes 리소스를 생성하고 상태를 추적합니다.
4. 사용자가 플랫폼을 통해 데스크톱에 접근합니다.
5. 관리자가 대시보드에서 상태와 용량을 모니터링합니다.

## 아키텍처

```text
Browser
  -> ClawManager Frontend
  -> ClawManager Backend
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

## 설정 메모

- 인스턴스 서비스는 Kubernetes 내부 네트워크에 유지됩니다
- 데스크톱 접근은 인증된 백엔드 프록시를 통해 전달됩니다
- 런타임 이미지는 시스템 설정에서 덮어쓸 수 있습니다
- 백엔드는 가능하면 클러스터 내부에 배치하는 것이 좋습니다

주요 백엔드 환경 변수:

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

### CSV 가져오기 템플릿

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

메모:

- `Email` 은 선택 사항입니다
- `Max GPU Count (optional)` 은 선택 사항입니다
- 나머지 열은 모두 필수입니다

## 라이선스

이 프로젝트는 MIT License로 배포됩니다.

## 오픈 소스

issue와 pull request를 환영합니다.
