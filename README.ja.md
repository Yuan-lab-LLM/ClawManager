# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  チーム規模からクラスター規模まで、OpenClaw と Linux デスクトップランタイムを一元管理するための Kubernetes-first コントロールプレーンです。
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
  <img src="https://img.shields.io/badge/ClawManager-Virtual%20Desktop%20Platform-e25544?style=for-the-badge" alt="ClawManager Platform" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
</p>

## News

- `2026-03-26`: AI Gateway のドキュメントと概要を更新し、モデルガバナンス、監査トレース、コスト計算、リスク制御を整理しました。詳しくは [AI Gateway](#ai-gateway) を参照してください。

## これは何か

ClawManager は、Kubernetes 上でデスクトップランタイムのデプロイ、運用、アクセスを一元化します。

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager Admin" width="32%" />
  <img src="./docs/main/portal.png" alt="ClawManager Portal" width="32%" />
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="32%" />
</p>

次のような環境に向いています。

- 複数ユーザー向けにデスクトップインスタンスを作成したい
- quota、イメージ、ライフサイクルを集中管理したい
- デスクトップサービスをクラスター内部に閉じ込めたい
- Pod を直接公開せず、安全なブラウザーアクセスを提供したい

## 選ばれる理由

- ユーザー、quota、インスタンス、ランタイムイメージをまとめて管理できる単一の管理画面
- OpenClaw のメモリや設定のインポート/エクスポートをサポート
- サービスを直接公開せず、プラットフォーム経由で安全にデスクトップへアクセス
- AI Gateway による制御されたモデルアクセス、監査トレース、コスト分析、リスク制御
- Kubernetes に自然に馴染むデプロイと運用フロー
- 管理者主導の展開にもセルフサービス型の利用にも対応

## クイックスタート

### 前提条件

- 利用可能な Kubernetes クラスター
- `kubectl get nodes` が正常に動作すること

### デプロイ

同梱のマニフェストをそのまま適用します。

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## ソースコードからビルド

同梱の Kubernetes マニフェストではなく、ソースコードから ClawManager を実行またはパッケージ化したい場合:

### フロントエンド

```bash
cd frontend
npm install
npm run build
```

### バックエンド

```bash
cd backend
go mod tidy
go build -o bin/clawreef cmd/server/main.go
```

### Docker イメージ

リポジトリルートでアプリ全体のイメージをビルドします。

```bash
docker build -t clawmanager:latest .
```

### デフォルトアカウント

- デフォルト管理者アカウント: `admin / admin123`
- インポートした管理者ユーザーのデフォルトパスワード: `admin123`
- インポートした一般ユーザーのデフォルトパスワード: `user123`

### 最初の使い方

1. 管理者としてログインします。
2. ユーザーを作成またはインポートし、quota を割り当てます。
3. システム設定でランタイムイメージカードを確認または更新します。
4. 一般ユーザーとしてログインし、インスタンスを作成します。
5. Portal View または Desktop Access からデスクトップにアクセスします。

## 主な機能

- インスタンスのライフサイクル管理: 作成、起動、停止、再起動、削除、参照、同期
- 対応ランタイム: `openclaw`、`webtop`、`ubuntu`、`debian`、`centos`、`custom`
- 管理画面からのランタイムイメージカード管理
- CPU、メモリ、ストレージ、GPU、インスタンス数に対するユーザー単位の quota 制御
- ノード、CPU、メモリ、ストレージを対象にしたクラスターリソース概要
- トークンベースのデスクトップアクセスと WebSocket 転送
- AI Gateway によるモデル管理、追跡可能な監査ログ、コスト計算、リスク制御
- CSV ベースの一括ユーザーインポート
- 多言語インターフェース

## AI Gateway

AI Gateway は、ClawManager におけるモデルアクセスのガバナンスプレーンです。OpenClaw インスタンスに単一の OpenAI 互換エントリーポイントを提供し、上流 Provider の上にポリシー、監査、コスト制御を追加します。

- 通常モデルとセキュアモデルの管理、Provider 接続、有効化、エンドポイント設定、価格ポリシー
- リクエスト、レスポンス、ルーティング判断、リスクヒットを対象にしたエンドツーエンドの監査/トレース記録
- トークン集計と利用見積もりを含む組み込みのコスト計算
- 設定可能なルールに基づくリスク制御と、`block` や `route_secure_model` などの自動アクション

スクリーンショット、詳細な機能説明、モデル選択とルーティングの流れについては [docs/aigateway.md](./docs/aigateway.md) を参照してください。

## 利用の流れ

1. 管理者がユーザー、quota、ランタイムイメージ方針を定義します。
2. ユーザーが OpenClaw または Linux デスクトップインスタンスを作成します。
3. ClawManager が Kubernetes リソースを作成し、状態を追跡します。
4. ユーザーがプラットフォーム経由でデスクトップにアクセスします。
5. 管理者がダッシュボードから健全性と容量を監視します。

## アーキテクチャ

```text
Browser
  -> ClawManager Frontend
  -> ClawManager Backend
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

## 設定メモ

- インスタンスサービスは Kubernetes の内部ネットワーク上で動作します
- デスクトップアクセスは認証済みバックエンドプロキシを経由します
- ランタイムイメージはシステム設定から上書きできます
- バックエンドはクラスター内部に配置するのが理想です

主なバックエンド環境変数:

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

### CSV インポートテンプレート

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

メモ:

- `Email` は任意です
- `Max GPU Count (optional)` は任意です
- それ以外の列は必須です

## ライセンス

このプロジェクトは MIT License の下で公開されています。

## オープンソース

issue と pull request を歓迎します。
