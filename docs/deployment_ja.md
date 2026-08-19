[← README に戻る](../README.ja.md)

# デプロイガイド

ClawManager は Kubernetes Native です。Distribution と Storage に合う Profile を選択します。

## Profile

| 環境 | Manifest | Storage |
|---|---|---|
| k3s Single Node | [`k3s/single-node`](../deployments/k3s/single-node/clawmanager.yaml) | Label した Node の HostPath |
| k3s Cluster | [`k3s/cluster`](../deployments/k3s/cluster/clawmanager.yaml) | CSI RWO + RWX |
| Kubernetes Single Node | [`k8s/single-node`](../deployments/k8s/single-node/clawmanager.yaml) | Label した Node の HostPath |
| Kubernetes Cluster | [`k8s/cluster`](../deployments/k8s/cluster/clawmanager.yaml) | CSI RWO + RWX |

完全な Profile を 1 つだけ使います。Longhorn 名は例であり同じ AccessMode の StorageClass に置換できます。Manifest を混ぜたり Multi-Node に一時 `/tmp` HostPath を使ったりしません。

## 内容と手順

Profile は ClawManager、MySQL、MinIO、Skill Scanner、Team Redis、Workspace Service、OpenClaw/Hermes/OpenCode Lite Pool を起動します。Lite Instance は Runtime Pod を共有します。

1. Secret、Image、StorageClass、外部 Access を確認。
2. Single Node の Label または Cluster の RWO/RWX CSI を確認。
3. 1 Profile を適用し Pod Ready と PVC Bound を待機。
4. 通常 Model を設定し AI Gateway と Security を確認。
5. 公開する Runtime ごとに Test Instance を作成。

新規 MySQL は `clawmanager-mysql-init` で初期化され、既存 Volume では First-Start Script を再実行しません。永続 Data に `emptyDir` を使いません。

## Storage と ARM64

Single-Node HostPath は固定 Node/Affinity が必要です。Multi-Node は本物の RWO/RWX CSI が必要で、`local-path` は Cluster RWX ではありません。Cluster Profile で暗黙 HostPath Fallback を有効にしません。

ClawManager と Skill Scanner は `linux/arm64` に対応しますが、MySQL、Redis、MinIO/Workspace、全 Runtime Image も確認します。Mixed Architecture は対応 Tag と Node Selector/Affinity を使い、SSD、十分な Memory、固定 Tag を推奨します。amd64 と同じ PVC、Desktop、Model、Skill の受入確認を行います。

## 受入確認

Pod/PVC、Web、Model、Security、AI Gateway、必要な Runtime を確認します。PVC Pending では StorageClass、PVC、Pod、Event、Describe を収集してから対処します。[ユーザーマニュアル](./use_guide_ja.md) も参照してください。
