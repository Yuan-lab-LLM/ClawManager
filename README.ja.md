# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  ClawManager は、AI エージェントインスタンス管理のための Kubernetes ネイティブなコントロールプレーンです。ガバナンス付きの AI アクセス、ランタイムオーケストレーション、そして複数の Agent Runtime にまたがる再利用可能なリソース管理を提供します。
</p>

<p align="center">
  <strong>言語:</strong>
  <a href="./README.md">English</a> |
  <a href="./README.zh-CN.md">简体中文</a> |
  日本語 |
  <a href="./README.ko.md">한국어</a> |
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
  <a href="#product-tour">製品紹介</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#runtime-integrations">Runtime 連携</a> |
  <a href="#resource-management">リソース管理</a> |
  <a href="#get-started">はじめに</a>
</p>

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
    <img src="https://img.shields.io/github/stars/Yuan-lab-LLM/ClawManager?style=for-the-badge&logo=github&label=Star%20ClawManager" alt="Star ClawManager on GitHub" />
  </a>
</p>

<h2 align="center">60 秒でわかる ClawManager</h2>

<p align="center">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-launch-60s-hd.gif" alt="ClawManager 製品デモ" width="100%" />
</p>

<p align="center">
  エージェントの高速プロビジョニング、Skill 管理とスキャン、AI Gateway ガバナンスを短時間で確認できます。
</p>

## 最新情報

最近の重要な製品アップデートとドキュメント更新です。

- [2026-04-29] Hermes Runtime 連携を追加しました。Webtop ベースのインスタンス作成、Agent Control Plane 登録、AI Gateway 注入、channel と skill のブートストラップ、`.hermes` のインポート/エクスポートに対応しています。詳しくは [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md) を参照してください。
- [2026-04-08] プラットフォームに Skill 管理と Skill スキャンのワークフローを追加しました。詳細は [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52) を参照してください。
- [2026-03-26] AI Gateway ドキュメントを更新し、モデルガバナンス、監査とトレース、コスト計算、リスク制御の説明を強化しました。詳しくは [AI Gateway Guide](./docs/aigateway.md) を参照してください。
- [2026-03-20] ClawManager は、AI エージェントワークスペース向けのより広いコントロールプレーンへと進化し、ランタイム制御、再利用可能なリソース、安全スキャンのワークフローを強化しました。

> ClawManager があなたのチームに役立つなら、ぜひ Star を付けて、より多くのユーザーや開発者に届くよう応援してください。

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>

## WeChatコミュニティグループ

ClawManager オープンソースコミュニティの WeChat グループです。プロダクト更新の確認、使い方の相談、コントリビューター同士の交流にご活用ください。

<p align="center">
  <img src="./docs/main/clawmanager_group_chat.jpg" alt="ClawManager WeChatグループのQRコード" width="300" />
</p>

<a id="product-tour"></a>
## 製品紹介

ClawManager は、AI エージェントインスタンスの運用を Kubernetes に持ち込み、そのランタイム基盤の上に 3 つの高次なコントロールプレーンを重ねます。チームはこれを使って AI アクセスを統制し、Agent を通じてランタイム動作を編成し、スキャン可能で再利用可能な channel と skill を用いてワークスペース機能を提供できます。

次のようなチームに向いています。

- 複数ユーザー向けに AI エージェントインスタンスを運用するプラットフォームチーム
- ランタイムの可観測性、コマンド配布、 desired state 管理が必要な運用チーム
- 手作業の設定ではなく、再利用可能なリソースで Agent ワークスペースを届けたい開発チーム

<a id="runtime-integrations"></a>
## Runtime 連携

ClawManager は現在、次の管理対象 Runtime をサポートします。

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`: ClawManager が管理するデスクトップインスタンスで使われる標準の OpenClaw スタイル Runtime
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`: 永続化された `.hermes` ワークスペースと内蔵 Hermes agent を備えた Webtop ベースの Runtime 連携

Runtime プレビュー:

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![openclaw](./docs/images/openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![hermes](./docs/images/hermes.png)

Runtime 開発者は、[Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md)、[Generic Runtime Agent Integration Guide](./docs/runtime-agent-integration-guide.md)、[Skill Content MD5 Spec](./docs/skill-content-md5-spec.md) を参照して互換 agent を実装できます。

<a id="get-started"></a>
## はじめに

ClawManager は、標準 Kubernetes と軽量クラスタの両方に対して、より明確な導入入口を提供します。まずは自分の環境に合うデプロイパスを選び、その後に初回ログインと基本操作のフローへ進むのがおすすめです。

- 標準 Kubernetes デプロイ: [deployments/k8s/clawmanager.yaml](./deployments/k8s/clawmanager.yaml)
- K3s / 軽量クラスタ向けデプロイ: [deployments/k3s/clawmanager.yaml](./deployments/k3s/clawmanager.yaml)
- 初回ログインと基本操作フロー: [ユーザーガイド](./docs/use_guide_ja.md)
- デプロイ説明とアーキテクチャ背景: [Deployment Guide (English)](./docs/deployment.md)

## 3 つのコントロールプレーン

<a id="ai-gateway"></a>
### AI Gateway

AI Gateway は、ClawManager におけるモデルアクセスのガバナンスプレーンです。管理対象の Agent Runtime に統一された OpenAI 互換エントリポイントを提供し、上流プロバイダの上にポリシー、監査、コスト制御を追加します。

- モデルトラフィックの統一エントリポイント
- セキュアモデルのルーティングとポリシー駆動のモデル選択
- エンドツーエンドの監査・トレース記録
- 組み込みのコスト計算と利用分析
- ブロックやルート変更を行えるリスク制御ルール

[AI Gateway Guide (English)](./docs/aigateway.md) を参照してください。

<a id="agent-control-plane"></a>
### Agent Control Plane

Agent Control Plane は、管理対象 AI エージェントインスタンスのランタイム編成レイヤーです。各インスタンスを、登録・状態報告・コマンド受信・プラットフォーム側 desired state への整合が可能な管理対象ランタイムへと変えます。

- セキュアなブートストラップとセッションライフサイクルによる Agent 登録
- ハートビートベースのランタイム状態とヘルス報告
- コントロールプレーンとインスタンス間の desired state 同期
- 起動、停止、設定適用、ヘルスチェック、Skill 操作のコマンド配布
- インスタンス単位での Agent 状態、channel、skill、コマンド履歴の可視化

[Agent Control Plane Guide (English)](./docs/agent-control-plane.md) を参照してください。

<a id="resource-management"></a>
### リソース管理

リソース管理は、AI エージェントワークスペース向けの再利用可能な資産レイヤーです。チームは channel や skill を準備し、bundle として組み合わせ、インスタンスへ注入し、安全レビューをその流れに組み込むことができます。

- `Channel` 管理: ワークスペース接続と統合テンプレート
- `Skill` 管理: 再利用可能な機能パッケージ
- `Skill Scanner` ワークフロー: リスク確認とスキャンジョブ
- bundle ベースのリソース構成: 再現性の高いセットアップ
- 注入スナップショットによる実適用内容の追跡

[Resource Management Guide (English)](./docs/resource-management.md) と [Security / Skill Scanner Guide (English)](./docs/security-skill-scanner.md) を参照してください。

## 製品ギャラリー

ClawManager は、管理、アクセス、AI ガバナンスを別々のツールとして扱うのではなく、ひとつの製品体験としてまとめるよう設計されています。

### 管理コンソール

管理コンソールでは、ユーザー、クォータ、ランタイム操作、セキュリティ制御、プラットフォームレベルのポリシーをひとつの画面に集約します。大規模な AI エージェント基盤を運用するチームの中心となる作業面です。

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager 管理コンソール" width="100%" />
</p>

### Portal Access

Portal は、ユーザーに一貫したワークスペース入口を提供します。ブラウザベースでアクセスしながら、コントロールプレーンと同期したランタイム状態を確認でき、インフラの細部を直接意識する必要はありません。

<p align="center">
  <img src="./docs/main/portal.png" alt="ClawManager Portal Access" width="100%" />
</p>

### AI Gateway

AI Gateway は、モデル利用のガバナンスをワークスペース体験そのものに統合します。監査ログ、コスト可視化、リスクルーティングを通じて、AI 利用を単発の統合ではなく、プラットフォーム機能として扱えるようにします。

<p align="center">
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="100%" />
</p>

## 動作の流れ

1. 管理者がガバナンスポリシーと再利用可能なリソースを定義します。
2. ユーザーが Kubernetes 上で管理対象の AI エージェントワークスペースを作成または利用します。
3. Agent がコントロールプレーンへ接続し、ランタイム状態を報告します。
4. Channel、skill、bundle がコンパイルされ、インスタンスへ適用されます。
5. AI トラフィックは AI Gateway を経由し、監査、リスク、コスト制御が付与されます。

## 開発者向け概要

ClawManager は、React フロントエンド、Go バックエンド、状態管理用 MySQL、そして `skill-scanner` やオブジェクトストレージ統合を含む Kubernetes ネイティブなプラットフォームです。コードベースは製品サブシステムごとに整理されているため、該当ガイドから入り、その後コードへ進むのが最も効率的です。

- フロントエンドの管理画面とユーザー画面は `frontend/`
- バックエンドのサービス、handler、repository、migration は `backend/`
- デプロイ資産は `deployments/`
- 製品ドキュメントと素材は `docs/`

[Developer Guide (English)](./docs/developer-guide.md) を参照してください。

## ドキュメント

- [ユーザーガイド](./docs/use_guide_ja.md)
- [Deployment Guide (English)](./docs/deployment.md)
- [Admin and User Guide (English)](./docs/admin-user-guide.md)
- [Agent Control Plane Guide (English)](./docs/agent-control-plane.md)
- [AI Gateway Guide (English)](./docs/aigateway.md)
- [Security / Skill Scanner Guide (English)](./docs/security-skill-scanner.md)
- [Resource Management Guide (English)](./docs/resource-management.md)
- [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md)
- [Generic Runtime Agent Integration Guide](./docs/runtime-agent-integration-guide.md)
- [Skill Content MD5 Spec](./docs/skill-content-md5-spec.md)
- [Developer Guide (English)](./docs/developer-guide.md)

## ライセンス

このプロジェクトは MIT License のもとで公開されています。

## オープンソース

Issue と Pull Request を歓迎します。

## Star History

<a href="https://www.star-history.com/?repos=Yuan-lab-LLM%2FClawManager&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
 </picture>
</a>
