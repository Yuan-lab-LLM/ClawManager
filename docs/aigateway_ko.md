[← README로 돌아가기](../README.ko.md)

# AI Gateway 사용자 가이드

AI Gateway는 OpenClaw, Hermes, OpenCode, DeepSeek Harness, Team과 Platform Function의 관리형 Model Access Layer입니다.

## Instance 생성 전

**AI Gateway → Models**에서 일반 Model을 하나 이상 추가·활성화하고 Health Test를 수행합니다. Security Model은 Risk Rule이 Sensitive Request를 별도 경로로 보낼 때만 필요합니다. Active Model이 없으면 Model 기반 Instance와 Custom Team을 만들 수 없습니다.

## 다섯 영역

- **Models**: Provider URL, Model, Credential, Protocol, Price, Health, Enable, Security Role, Thinking.
- **AI Audit**: Trace, Routing, Provider Response, Policy Hit, Latency, Error.
- **Costs**: Token 추정과 내부 Accounting.
- **Session Usage**: User, Runtime, Instance, Session별 사용량.
- **Risk Rules**: 순서가 있는 Allow, Block, Secure Route.

Managed Thinking은 ClawManager가 안정적으로 제어할 수 있는 조합의 영구 설정입니다. Latency와 Reasoning Token이 늘 수 있지만 비공개 사고 내용을 표시하지 않습니다. Model 설정이 Off이면 Runtime이 임의로 다시 켤 수 없습니다.

## Protocol과 Routing

OpenAI Chat Completions, OpenAI Responses, Anthropic Messages를 지원합니다. Upstream Provider에 맞는 Protocol을 선택하고 Streaming과 Tool Call을 운영 전 확인하세요. Risk Rule은 순서대로 Block, Active Security Model Route, 일반 Model 유지를 결정합니다.

## Session Usage

기간, User, Runtime, Instance, Session으로 필터하고 Runtime/Provider가 보고한 Input, Output, Cached, Reasoning Token과 설정 가격 기반 추정 비용을 비교합니다. Request Routing, Error, Policy 근거는 **AI Audit**에서 확인합니다.

Session Usage는 관측 화면이며 대화 편집기나 Provider 최종 청구서가 아닙니다. 오래되었거나 중단되었거나 미지원인 Session은 불완전할 수 있고 Retry와 Token 분류 차이로 합계가 다를 수 있습니다.

## 문제 해결

| 증상 | 확인 |
|---|---|
| Creation에 Model 없음 | 일반 Model Active/Healthy. |
| Thinking 비활성 | Provider/Model이 관리 대상이 아님. |
| Session Usage 비어 있음 | 기간, Filter, Runtime Report. |
| Cost 없음 | Model Input/Output Price. |
| 예상 밖 Block/Route | AI Audit의 Risk Rule과 순서. |

[사용자 매뉴얼](./use_guide_ko.md)과 [Security Protection](./security-platform_ko.md)도 참고하세요.
