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
  <a href="https://discord.gg/9RwgbGJD5R">
    <img src="https://img.shields.io/badge/Discord-Join%20Us-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="ClawManager Discord コミュニティに参加" />
  </a>
</p>

<p align="center">
  <a href="#product-tour">製品紹介</a> |
  <a href="#team-workspaces">Team ワークスペース</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#runtime-integrations">Runtime 連携</a> |
  <a href="#resource-management">リソース管理</a> |
  <a href="#security-protection-platform">Security Protection</a> |
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

- [2026-08-19] 管理対象 OpenCode ワークスペース、新しいインスタンス画面、OpenClaw・Hermes・OpenCode 向け Skill Hub 配布を追加しました。[OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_ja.md) を参照してください。
- [2026-08-18] 8 個の読み取り専用テンプレート、自然言語によるカスタム Team、Hermes Lite Worker、ライブ Kanban、共有成果物、メンバーセッションで Team コラボレーションを強化しました。
- [2026-08-17] モデル管理 Thinking、AI Gateway Session Usage、スケジュールタスク編集、Lite のライフサイクルと一括操作を追加しました。
- [2026-08-16] DeepSeek Harness Lite / Pro を追加し、共有 Runtime Pool の分離、専用 Webtop デスクトップ、AI Gateway モデル注入、Skill・Workspace 統合、Lite 専用 Browser Origin に対応しました。
- [2026-07-07] セキュリティ保護プラットフォーム（secplane）フロントエンドコンソールを追加しました。ランタイム防御（入力/状態/決定/出力サーフェス、資産改ざん防止、ヒューマン承認）、ホスト強化とコンテナ分離、アウトバウンド信頼エンドポイントガバナンス、ポリシーガバナンス、キルスイッチ/サーキットブレーカー、フルチェーン監査、SecureClaw データ・コンポーネント信頼監査、コラボレーションガバナンス、入力検出をカバーする4層防御の統合管理UIを5言語i18nで提供します。
- [2026-06-14] Lite / Pro ランタイムモードとロールアウト対応を追加しました。Lite インスタンスは共有 gateway runtime pool で動作し、Pro インスタンスはより強い分離のため専用 desktop deployment を維持します。
- [2026-05-18] Team ワークスペース MVP の紹介とプレビューを追加しました。ワンクリック Team 作成、OpenClaw メンバーのオーケストレーション、Redis Team Bus 注入、共有ストレージ、メンバー状態、タスク配布、イベント/結果ビューをカバーします。
- [2026-04-29] Hermes Runtime 連携を追加しました。Webtop ベースのインスタンス作成、Agent Control Plane 登録、AI Gateway 注入、channel と skill のブートストラップ、`.hermes` のインポート/エクスポートに対応しています。[ユーザーマニュアル](./docs/use_guide_ja.md#create-a-workspace) を参照してください。
- [2026-04-08] プラットフォームに Skill 管理と Skill スキャンのワークフローを追加しました。詳細は [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52) を参照してください。
- [2026-03-26] AI Gateway ドキュメントを更新し、モデルガバナンス、監査とトレース、コスト計算、リスク制御の説明を強化しました。詳しくは [AI Gateway Guide](./docs/aigateway_ja.md) を参照してください。
- [2026-03-20] ClawManager は、AI エージェントワークスペース向けのより広いコントロールプレーンへと進化し、ランタイム制御、再利用可能なリソース、安全スキャンのワークフローを強化しました。

> ClawManager があなたのチームに役立つなら、ぜひ Star を付けて、より多くのユーザーや開発者に届くよう応援してください。

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>

## コミュニティ

ClawManager オープンソースコミュニティに WeChat または Discord から参加してください。プロダクト更新の確認、使い方の相談、コントリビューター同士の交流にご活用ください。

<table align="center">
  <tr>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_group_chat.jpg" alt="ClawManager WeChatグループのQRコード" height="300" />
      <br /><br />
      <strong>WeChat</strong>
      <br />
      QRコードをスキャンして WeChat グループに参加
    </td>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_discord.jpg" alt="ClawManager Discord 招待QRコード" height="300" />
      <br /><br />
      <strong>Discord</strong>
      <br />
      <a href="https://discord.gg/9RwgbGJD5R">QRコードをスキャンして Discord サーバーに参加</a>
    </td>
  </tr>
</table>

<a id="product-tour"></a>
## 製品紹介

ClawManager は、管理対象 Runtime、Team、モデルアクセス、リソースと Skill Hub、プラットフォームセキュリティを 1 つの Kubernetes ネイティブ製品に統合します。

次のようなチームに向いています。

- 複数ユーザー向けに AI エージェントインスタンスを運用するプラットフォームチーム
- ランタイムの可観測性、コマンド配布、 desired state 管理が必要な運用チーム
- 手作業の設定ではなく、再利用可能なリソースで Agent ワークスペースを届けたい開発チーム

<a id="runtime-integrations"></a>
## Runtime 連携

ClawManager は現在、次の管理対象 Runtime をサポートします。

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`: Lite / Pro、ネイティブ会話、ツール、スケジュールタスク、Team 対応
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`: Lite / Pro、永続 `.hermes`、ネイティブセッション、Team Worker 対応
- <img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> `OpenCode`: AI Gateway、デスクトップ/ターミナル、ファイルを備えた管理対象コーディング環境。[OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_ja.md)
- <img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> `DeepSeek Harness`: Lite 共有 Pool と Pro 専用 Desktop、AI Gateway モデル注入、Skill、Workspace File、分離 Browser Access に対応

Runtime プレビュー:

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![OpenClaw workspace](./docs/main/runtime-openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![Hermes workspace](./docs/main/runtime-hermes.png)

**<img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> OpenCode**

![OpenCode workspace](./docs/main/runtime-opencode.png)

**<img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> DeepSeek Harness**

![DeepSeek Harness ワークスペース](./docs/main/runtime-deepseek-harness.png)

<a id="get-started"></a>
## はじめに

まず `k3s` または `k8s` を選び、次に単一ノードまたはクラスタ向けのストレージ構成を選びます。

- k3s 単一ノード / HostPath: [Manifest](./deployments/k3s/single-node/clawmanager.yaml)
- k3s クラスタ / CSI-RWX: [Manifest](./deployments/k3s/cluster/clawmanager.yaml)
- Kubernetes 単一ノード / HostPath: [Manifest](./deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes クラスタ / CSI-RWX: [Manifest](./deployments/k8s/cluster/clawmanager.yaml)
- 初回ログインと基本操作フロー: [ユーザーガイド](./docs/use_guide_ja.md)
- デプロイ説明とアーキテクチャ背景: [Deployment Guide](./docs/deployment_ja.md)

## プラットフォームの主要機能

### Runtime とインスタンス管理

OpenClaw、Hermes、OpenCode、DeepSeek Harness を Lite / Pro で作成し、イメージ、リソース、ライフサイクル、デスクトップ、ファイル、Shell、環境変数、アーカイブ、Share Link、一括操作を管理できます。

<a id="ai-gateway"></a>
### AI Gateway

AI Gateway は Models、AI Audit、Costs、Session Usage、Risk Rules の 5 領域を提供します。Chat Completions、OpenAI Responses、Anthropic Messages と、対応モデルの管理対象 Thinking を扱います。

- モデルトラフィックの統一エントリポイント
- セキュアモデルのルーティングとポリシー駆動のモデル選択
- エンドツーエンドの監査・トレース記録
- 組み込みのコスト計算と利用分析
- ブロックやルート変更を行えるリスク制御ルール

[AI Gateway Guide](./docs/aigateway_ja.md) を参照してください。

<a id="agent-control-plane"></a>
### Agent Control Plane

Agent Control Plane は、管理対象 AI エージェントインスタンスのランタイム編成レイヤーです。各インスタンスを、登録・状態報告・コマンド受信・プラットフォーム側 desired state への整合が可能な管理対象ランタイムへと変えます。

- セキュアなブートストラップとセッションライフサイクルによる Agent 登録
- ハートビートベースのランタイム状態とヘルス報告
- コントロールプレーンとインスタンス間の desired state 同期
- 起動、停止、設定適用、ヘルスチェック、Skill 操作のコマンド配布
- インスタンス単位での Agent 状態、channel、skill、コマンド履歴の可視化

Lifecycle、Status、Restart、Runtime Health、管理操作は [ユーザーマニュアル](./docs/use_guide_ja.md#operate-an-instance) を参照してください。

<a id="resource-management"></a>
### リソース管理

リソース管理は、リソース、リソースパック、注入記録の 3 タブで構成されるユーザー向け OpenClaw 設定センターです。管理者向け Security Protection とは独立しています。

- Channel Template、Form/JSON 編集、複製、Lifecycle 管理
- Skill ZIP の Import、競合解決、Download、削除。Skill Hub は Catalog、Version、Publish、Install を担当
- Scheduled Task の簡易/高度な編集。Agent Resource は表示されますが、現在この画面では設定できません
- Resource Pack の作成、編集、複製、インスタンス作成時の再利用
- 配布 Mode、Resource、環境変数、Status、作成時刻を示す読み取り専用の Injection Record

[Resource Management Guide](./docs/resource-management_ja.md) と [Skill Hub Guide](./docs/skill-hub-guide_ja.md) を参照してください。

<a id="team-workspaces"></a>
### Team コラボレーション

Team は Leader 仲介型の Flow です。8 個の変更不可 Built-in Template または User Custom Template から作成し、OpenClaw Lite Leader が計画、Task 分解、Dispatch、Member Delivery 検証、最終結果の公開を行います。

- Team ごとに OpenClaw Lite Leader は 1 名、Worker は OpenClaw Lite または Hermes Lite
- Custom Team は自然言語で生成し、Role ごとに調整、全体再生成、再利用が可能
- Team Chat は Plan、Assignment、Progress、Review、Delivery、Final Synthesis を記録
- Execution Kanban は Current Query、Task Breakdown、Delivery State を表示
- Shared File/Artifact を保持し、Hermes Lite の Native Team Session は Instance View で確認可能

作成、協働段階、結果の確認方法は [Team Workspace Quick Guide](./docs/team-workspaces-guide_ja.md) を参照してください。

<a id="security-protection-platform"></a>
### Security Protection Platform

Security Protection は 4 つの Live 指標、Security Event、Pod Live Aegis Configuration、Report Export、Emergency Circuit Breaker を備えた独立 Admin Workspace です。Overview は現在 KSecure を 7 Risk Surface、15 Scenario、4 Layer と表示し、Runtime 防御、Host/Container 分離、Component Trust、Identity/Outbound、Policy、Collaboration、Quota、Approval、Skill Scanner、Full-chain Audit へ移動できます。

[Security Platform Guide](./docs/security-platform_ja.md) を参照してください。

## 製品ギャラリー

ClawManager は、管理、アクセス、AI ガバナンスを別々のツールとして扱うのではなく、ひとつの製品体験としてまとめるよう設計されています。

### Lite モードデプロイ

Lite モードは OpenClaw、Hermes、OpenCode、DeepSeek Harness を共有 gateway runtime pool 経由でプロビジョニングします。各ワークスペースは管理された runtime Pod 内の独立した gateway プロセスとして動作するため、起動が速く、専用 CPU、メモリ、ストレージ、GPU 割り当ての負担を抑えながら、ワークスペースアクセス、Share Link / Password アクセス、対応する channel と skill の注入、管理画面での可視性を維持します。

![](./docs/main/liteopenclaw.png)

### Pro モードデプロイ

Pro モードは各インスタンスに専用 desktop runtime をプロビジョニングし、独自の Kubernetes Deployment、Service、PVC で構成します。より強い分離、フルデスクトップリソース、runtime events、インスタンス単位の skill 管理、完全なデスクトップ管理体験が必要な場合に適しています。

![](./docs/main/proopenclaw.png)

### Team ワークスペース

Team ワークスペースは、左に会話と成果物、右に現在の問い合わせ、タスク分解、状態、成果物詳細を表示します。

<p align="center">
  <img src="./docs/main/team-collaboration.png" alt="ClawManager Team ワークスペース" width="100%" />
</p>

### リソース管理

Channel、Skill、Scheduled Task、Resource Pack、Injection Record を一つのユーザー画面で管理し、Security Protection は独立した管理者機能として扱います。

<p align="center">
  <img src="./docs/main/resource-management-current.png" alt="ClawManager リソース管理" width="100%" />
</p>

### Security Protection

専用の管理画面で Live 指標と Event、KSecure Layer、Pod Aegis、Report Export、Emergency Circuit Breaker を操作します。

<p align="center">
  <img src="./docs/main/security-protection-current.png" alt="ClawManager Security Protection" width="100%" />
</p>

### 管理コンソール

管理コンソールでは、ユーザー、クォータ、ランタイム操作、セキュリティ制御、プラットフォームレベルのポリシーをひとつの画面に集約します。大規模な AI エージェント基盤を運用するチームの中心となる作業面です。

<p align="center">
  <img src="./docs/main/admin-current.png" alt="ClawManager 管理コンソール" width="100%" />
</p>

### Portal Access

Portal は、ユーザーに一貫したワークスペース入口を提供します。ブラウザベースでアクセスしながら、コントロールプレーンと同期したランタイム状態を確認でき、インフラの細部を直接意識する必要はありません。

<p align="center">
  <img src="./docs/main/portal-current.png" alt="ClawManager Portal Access" width="100%" />
</p>

### AI Gateway

AI Gateway は、モデル利用のガバナンスをワークスペース体験そのものに統合します。監査ログ、コスト可視化、リスクルーティングを通じて、AI 利用を単発の統合ではなく、プラットフォーム機能として扱えるようにします。

<p align="center">
  <img src="./docs/main/ai-gateway-current.png" alt="ClawManager AI Gateway" width="100%" />
</p>

## 動作の流れ

1. 管理者がガバナンスポリシーと再利用可能なリソースを定義します。
2. ユーザーが Kubernetes 上で管理対象の AI エージェントワークスペースを作成または利用します。
3. Team ワークスペースは、複数のメンバー Runtime を Redis Team Bus と共有ストレージ設定付きでプロビジョニングできます。
4. Agent がコントロールプレーンへ接続し、ランタイム状態を報告します。
5. Channel、skill、bundle がコンパイルされ、インスタンスへ適用されます。
6. AI トラフィックは AI Gateway を経由し、監査、リスク、コスト制御が付与されます。

## 開発者向け概要

ClawManager は、React フロントエンド、Go バックエンド、状態管理用 MySQL、そして `skill-scanner` やオブジェクトストレージ統合を含む Kubernetes ネイティブなプラットフォームです。コードベースは製品サブシステムごとに整理されているため、該当ガイドから入り、その後コードへ進むのが最も効率的です。

- フロントエンドの管理画面とユーザー画面は `frontend/`
- バックエンドのサービス、handler、repository、migration は `backend/`
- デプロイ資産は `deployments/`
- 製品ドキュメントと素材は `docs/`

Runtime と Protocol の技術資料は Contributor 向けに `docs/` に残し、以下のユーザードキュメントは製品 Workflow ごとに整理しています。

## ドキュメント

- [ユーザーガイド](./docs/use_guide_ja.md)
- [Team Workspace Quick Guide](./docs/team-workspaces-guide_ja.md)
- [Deployment Guide](./docs/deployment_ja.md)
- [AI Gateway Guide](./docs/aigateway_ja.md)
- [Security Platform Guide](./docs/security-platform_ja.md)
- [Resource Management Guide](./docs/resource-management_ja.md)
- [Skill Hub Guide](./docs/skill-hub-guide_ja.md)
- [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_ja.md)

## ライセンス

このプロジェクトは MIT License のもとで公開されています。

## オープンソース

Issue と Pull Request を歓迎します。

## Star History

<a href="https://github.com/Yuan-lab-LLM/ClawManager/actions/workflows/update-star-history.yml">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
   <img alt="Star History Chart" src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
 </picture>
</a>
