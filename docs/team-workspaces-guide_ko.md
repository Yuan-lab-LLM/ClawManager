# Team 워크스페이스 빠른 가이드

Team은 한 명의 OpenClaw Lite Leader가 여러 Worker를 조율해 공동 목표를 수행하도록 합니다. 변경할 수 없는 기본 제공 템플릿을 사용하거나 자연어 의도에서 사용자 전용 Team 템플릿을 생성할 수 있습니다. Leader는 목표를 이해하고, 작업을 배정하고, 멤버 산출물을 수집하고, 예외를 처리한 뒤 최종 결과를 제공합니다.

## 적용 범위

- 협업 방식은 **Leader 중개 협업**으로 고정됩니다. 사용자 요청은 먼저 Leader에게 전달되고 Leader가 멤버를 조율합니다.
- Leader는 항상 **OpenClaw Lite**를 사용합니다. 각 Worker는 **OpenClaw Lite** 또는 **Hermes Lite**를 선택할 수 있습니다.
- 활성화된 Hermes Lite gateway 이미지가 없으면 Hermes Lite 선택이 비활성화되고 이유가 표시됩니다.
- 기본 제공 템플릿은 수정하거나 삭제할 수 없습니다. 사용자 정의 템플릿은 현재 사용자 소유이며 조정, 삭제, 재사용할 수 있습니다.

## 1. 기본 제공 템플릿으로 Team 만들기

1. 탐색 메뉴에서 **Teams**를 열고 생성 페이지로 이동합니다.
2. Team 이름을 입력하고 필요하면 공유 저장 공간을 조정합니다.
3. 템플릿을 선택하고 각 Worker에 사용 가능한 Runtime을 선택합니다.
4. 요약을 확인한 후 **생성**을 선택합니다.

오른쪽 위의 **+ 사용자 정의 Team**에서 사용자 정의 템플릿 관리로 이동할 수 있습니다. 같은 페이지의 멤버 표에서 Worker Runtime을 선택합니다.

![기본 제공 템플릿, 사용자 정의 Team 진입점, Worker Runtime 선택](./main/team-create-fixed-and-custom-entry.png)

변경할 수 없는 기본 템플릿은 8개입니다. Standard Two-Member, Delivery Three-Member, Product Discovery Four-Member, Quality Gate Four-Member, Full-stack Delivery Five-Member, API Integration Five-Member, Research Publication Six-Member, Software Engineering Eight-Member가 포함됩니다. 각 템플릿에 역할과 책임이 이미 포함되어 있어 멤버별 리소스 프리셋을 따로 설정할 필요가 없습니다.

## 2. 사용자 정의 Team 생성하기

**사용자 정의 Team**을 열고 Team이 달성할 목표를 설명합니다. 인원수를 비워 두면 시스템이 자동으로 결정하며, 직접 지정할 때는 총 2~6명을 선택할 수 있습니다.

![자연어와 인원수로 사용자 정의 Team 생성](./main/custom-team-generate.png)

생성과 책임 조정은 현재 사용자가 이용할 수 있는 AI Gateway를 `model: "auto"`로 사용합니다. 실제 모델은 Gateway가 선택하며 해당 모델에 저장된 Thinking 설정이 적용됩니다. 사용자 정의 Team 페이지에는 별도의 Thinking 스위치가 없습니다. 사용할 수 있는 모델이 없으면 모델 관리에서 모델을 활성화하라는 안내가 표시됩니다.

모든 생성 결과는 다음 규칙을 유지합니다.

- Team 인원은 2~6명입니다.
- 첫 번째이자 유일한 Leader는 `memberId=leader`를 유지합니다.
- 능력 태그는 적합한 능력을 설명할 뿐 Skill을 설치하거나 Runtime 설정을 변경하지 않습니다.

## 3. 사용자 정의 템플릿 관리하기

**내 사용자 정의 Team**에서 템플릿을 선택하면 다음 작업을 할 수 있습니다.

- 템플릿 이름 변경.
- 의도 또는 인원수를 바꾼 뒤 Team 전체 업데이트.
- 저장된 의도와 인원수로 Team 전체 재생성.
- 템플릿 삭제 또는 Team 생성 페이지에서 사용.

![기존 사용자 정의 Team 템플릿 관리](./main/custom-team-manage.png)

업데이트할 때마다 새 버전이 만들어집니다. 기본 제공 템플릿은 편집 목록에 나타나지 않습니다.

## 4. 멤버 책임 조정하기

멤버를 펼치고 원하는 변경 사항을 자연어로 설명합니다. 입력이 비어 있으면 명확한 안내가 표시되고 제출되지 않습니다.

![자연어로 멤버 책임 조정](./main/custom-team-member-adjustment.png)

Leader도 조정할 수 있지만 업무 영역의 확장 책임만 변경됩니다. Leader 신원, 고정 오케스트레이션 능력, 현재 Worker 명단, 배정, 수집, 검토, 최종 종합 관계는 유지됩니다. Worker 수가 바뀌어도 기존 Team 초기화 흐름을 통해 Leader는 전체 멤버와 책임을 전달받습니다.

## 5. 협업 시작 및 진행 상황 확인

생성 후 Team 채팅에서 Leader에게 목표를 설명합니다. Leader는 계획, 배정, 산출물과 Review 근거 수집, 최종 종합을 수행합니다. Worker 완료는 해당 Worker의 작업 항목만 닫으며, 루트 작업은 Leader가 종합한 뒤 완료됩니다.

Team 상세 페이지에는 다음 영역이 있습니다.

- **Team 채팅**: 계획, 배정, 유효한 진행, 산출물, Review, 최종 종합을 표시합니다.
- **Execution Kanban**: 헤더에 현재 Query를 표시하고 루트 작업과 멤버 작업 상태를 보여 줍니다.
- **Query 탐색**: Query가 2개 이상일 때 사용할 수 있으며 새 Query가 기본으로 선택됩니다.
- **Files**: 공유 산출물을 확인합니다. Markdown, 텍스트, JSON은 페이지 안에서 미리 볼 수 있고 다른 파일은 다운로드할 수 있습니다.

Monitor는 활동, 완료 영수증, 오류 신호를 관찰해 알림과 복구에 사용하지만 작업의 성공, 실패, 취소 또는 완료를 독자적으로 만들지 않습니다.

## 6. Hermes Lite Worker 세션

Hermes Lite Worker의 Team 대화는 Hermes 네이티브 세션 저장소를 사용합니다. 실행 중인 완전한 메시지와 도구 결과가 Hermes GUI에 점진적으로 표시되므로 작업 종료 후의 기록을 기다릴 필요가 없습니다.

Team 멤버 상세 또는 인스턴스 목록에서 같은 Hermes Lite 인스턴스를 열면 동일한 Team 세션을 보고 계속 대화할 수 있습니다. 일반 Hermes 세션의 동작은 그대로 유지됩니다. 세션은 상호작용과 관찰을 위한 것이며 Kanban과 완료 판정은 계속 Team 제어 영역을 기준으로 합니다.

## 7. 사용 권장 사항

- 먼저 가장 가까운 기본 템플릿을 선택하고 반복해서 사용할 전문 분업에는 사용자 정의 템플릿을 생성합니다.
- 목표에 범위, 데이터 출처, 출력 형식, 인수 기준을 명확히 적습니다.
- Worker가 산출물을 제출했다는 이유만으로 같은 요청을 다시 보내지 말고 Leader의 검토와 종합을 기다립니다.
- Thinking은 지연 시간과 추론 Token을 늘릴 수 있습니다. 작업에 맞게 모델 관리에서 설정하세요.
