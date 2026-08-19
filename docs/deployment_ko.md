[← README로 돌아가기](../README.ko.md)

# 배포 가이드

ClawManager는 Kubernetes Native 플랫폼입니다. Distribution과 Storage에 맞는 Profile을 선택하세요.

## Profile

| 환경 | Manifest | Storage |
|---|---|---|
| k3s Single Node | [`k3s/single-node`](../deployments/k3s/single-node/clawmanager.yaml) | Label된 Node HostPath |
| k3s Cluster | [`k3s/cluster`](../deployments/k3s/cluster/clawmanager.yaml) | CSI RWO + RWX |
| Kubernetes Single Node | [`k8s/single-node`](../deployments/k8s/single-node/clawmanager.yaml) | Label된 Node HostPath |
| Kubernetes Cluster | [`k8s/cluster`](../deployments/k8s/cluster/clawmanager.yaml) | CSI RWO + RWX |

하나의 완전한 Profile만 사용합니다. Longhorn 이름은 예시이며 같은 AccessMode의 StorageClass로 바꿀 수 있습니다. Manifest를 섞거나 Multi-Node에 임시 `/tmp` HostPath를 사용하지 마세요.

## 구성과 절차

Profile은 ClawManager, MySQL, MinIO, Skill Scanner, Team Redis, Workspace Service, OpenClaw/Hermes/OpenCode Lite Pool을 실행합니다. Lite Instance는 Runtime Pod를 공유합니다.

1. Secret, Image, StorageClass, 외부 Access 확인.
2. Single Node Label 또는 Cluster RWO/RWX CSI 확인.
3. 한 Profile 적용 후 Pod Ready와 PVC Bound 대기.
4. 일반 Model 설정 후 AI Gateway와 Security 확인.
5. 공개할 Runtime별 Test Instance 생성.

새 MySQL은 `clawmanager-mysql-init`로 초기화되며 기존 Volume은 First-Start Script를 다시 실행하지 않습니다. 영구 Data에 `emptyDir`를 사용하지 마세요.

## Storage와 ARM64

Single-Node HostPath는 고정 Node/Affinity가 필요합니다. Multi-Node는 실제 RWO/RWX CSI가 필요하며 `local-path`는 Cluster RWX가 아닙니다. Cluster Profile에서 암묵적 HostPath Fallback을 사용하지 마세요.

ClawManager와 Skill Scanner는 `linux/arm64`를 지원하지만 MySQL, Redis, MinIO/Workspace, 모든 Runtime Image도 확인해야 합니다. Mixed Architecture에서는 호환 Tag와 Node Selector/Affinity를 사용하고 SSD, 충분한 Memory, 고정 Tag를 권장합니다. amd64와 같은 PVC, Desktop, Model, Skill 검수를 수행하세요.

## 검수

Pod/PVC, Web, Model, Security, AI Gateway, 필요한 Runtime을 확인합니다. PVC Pending이면 StorageClass, PVC, Pod, Event, Describe를 수집한 뒤 처리하세요. [사용자 매뉴얼](./use_guide_ko.md)도 참고하세요.
