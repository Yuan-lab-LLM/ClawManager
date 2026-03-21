# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  OpenClaw のクラスタ一括配備と運用のために設計された世界初のプラットフォームです。
</p>

<p align="center">
  <strong>言語:</strong>
  <a href="./README.md">English</a> |
  <a href="./README.zh-CN.md">中文</a> |
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

<p align="center">
  <img src="https://img.shields.io/badge/OpenClaw-Desktop-f97316?style=flat-square&logo=linux&logoColor=white" alt="OpenClaw Desktop" />
  <img src="https://img.shields.io/badge/Webtop-Browser%20Desktop-0f766e?style=flat-square&logo=firefoxbrowser&logoColor=white" alt="Webtop" />
  <img src="https://img.shields.io/badge/Proxy-Secure%20Access-7c3aed?style=flat-square&logo=nginxproxymanager&logoColor=white" alt="Secure Proxy" />
  <img src="https://img.shields.io/badge/WebSocket-Realtime-2563eb?style=flat-square&logo=socketdotio&logoColor=white" alt="WebSocket" />
  <img src="https://img.shields.io/badge/i18n-5%20Languages-db2777?style=flat-square&logo=googletranslate&logoColor=white" alt="5 Languages" />
</p>

## 🚀 News

- [03/20/2026] **ClawManager 新規リリース** - ClawManager は仮想デスクトップ管理プラットフォームとして正式にリリースされ、バッチデプロイ、Webtop 対応、デスクトップ Portal アクセス、ランタイムイメージ設定、OpenClaw の記憶・設定 Markdown バックアップ移行、クラスタ資源概要、多言語ドキュメントを提供します。

## 👀 Overview

ClawManager は Kubernetes 向けの仮想デスクトップ管理プラットフォームです。デスクトップランタイム運用、ユーザー統制、安全なクラスタ内アクセスを含む完全な制御プレーンを提供します。

ClawManager はバッチデプロイ、インスタンスライフサイクル管理、管理コンソール、プロキシベースのデスクトップアクセス、ランタイムイメージ制御、クラスタ資源の可視化、OpenClaw の記憶・設定バックアップ移行機能を一つのプラットフォームに統合しています。

ClawManager は次のような環境を想定しています：

- 複数ユーザー向けに仮想デスクトップインスタンスを作成・管理したい
- 管理者が quota、イメージ、インスタンスを集中的に統制したい
- デスクトップサービスを Kubernetes 内部に保持し、認証付きプロキシ経由で公開したい
- オペレーターがインスタンス健全性、クラスタ容量、ランタイム状態を一元的に把握したい

要するに ClawManager は：

- OpenClaw と Linux デスクトップランタイムの集中運用コンソール
- Kubernetes 上のマルチユーザーデスクトップ管理プラットフォーム
- トークン認証プロキシによる内部デスクトップ向け安全アクセス層

## ✨ At a Glance

- マルチテナントなデスクトップインスタンス管理
- ユーザー単位またはランタイムプロファイル単位でのデスクトップ一括デプロイ
- CPU、メモリ、ストレージ、GPU、インスタンス数に対するユーザー quota 制御
- OpenClaw、Webtop、Ubuntu、Debian、CentOS、カスタムランタイムをサポート
- トークン生成と WebSocket 転送による安全なデスクトッププロキシアクセス
- OpenClaw の記憶、設定、Markdown 構成データのバックアップと移行
- ユーザー、インスタンス、イメージカード、クラスタ資源向けの管理ダッシュボード
- 多言語 UI：英語、中国語、日本語、韓国語、ドイツ語

> 🧭 ClawManager は管理制御、安全なデスクトップアクセス、ランタイム運用を一つの制御プレーンにまとめます。

<p align="center">
  <img src="frontend/public/clawmanager_overview.png" alt="ClawManager Overview" width="100%" />
</p>

## 📚 Table of Contents

- [News](#news)
- [Overview](#overview)
- [ClawManager New Features](#clawmanager-new-features)
- [Key Features](#key-features)
- [Typical Workflow](#typical-workflow)
- [Architecture](#architecture)
- [Project Structure](#project-structure)
- [Tech Stack](#tech-stack)
- [Kubernetes Prerequisites](#kubernetes-prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [License](#license)

## 🆕 ClawManager New Features

ClawManager の主要機能：

- 🖥 ブラウザ内デスクトップアクセス向け `webtop` ランタイム対応
- 📦 大規模デスクトップ展開向けのバッチデプロイ機能
- 🚪 実行中インスタンスを一箇所で切り替えられる Desktop Portal ページ
- 🔐 トークンベースのインスタンスアクセスエンドポイントとリバースプロキシ経路
- 🔄 デスクトップセッションおよび状態更新のための WebSocket 転送
- 🧠 OpenClaw の記憶、設定、Markdown 構成データのバックアップ/インポート API
- 🧩 対応インスタンスタイプごとのランタイムイメージカード管理
- 📊 ノード、CPU、メモリ、ストレージを対象としたクラスタ資源概要
- 👨‍💼 ユーザー横断でのフィルタと操作が可能なグローバル管理者インスタンス管理
- 📥 デフォルトパスワード生成付き CSV ユーザーインポート
- 🌍 5 言語対応の国際化フロントエンド

## 🛠 Key Features

- ⚙️ インスタンスライフサイクル管理: 作成、起動、停止、再起動、削除、参照、強制同期
- 📦 大規模デスクトップ展開を支えるバッチデプロイ対応
- 🧱 対応ランタイムタイプ: `openclaw`、`webtop`、`ubuntu`、`debian`、`centos`、`custom`
- 🔒 認証付きプロキシエンドポイントによる安全なデスクトップアクセス
- 📡 WebSocket ベースのリアルタイム状態更新
- 📝 OpenClaw の記憶、設定、Markdown 構成データのアーカイブバックアップ/インポート
- 📏 インスタンス数、CPU、メモリ、ストレージ、GPU に対するユーザー単位 quota 管理
- 🖼 管理パネルからのランタイムイメージ上書き管理
- 🛰 クラスタ資源概要とインスタンス健全性を提供する管理ダッシュボード
- 👥 CSV による一括ユーザー導入と集中 quota 割り当て
- 🌐 多言語 UI と管理者/一般ユーザーのロール別ビュー

## 🔄 Typical Workflow

1. 👨‍💼 管理者がログインし、ユーザー、quota、ランタイムイメージ設定を構成します。
2. 🖥 ユーザーが OpenClaw、Webtop、Ubuntu などのデスクトップインスタンスを作成します。
3. ☸️ ClawManager が Kubernetes リソースを作成し、ランタイム状態を同期し続けます。
4. 🔐 ユーザーは Portal またはトークンベースのプロキシエンドポイント経由でデスクトップにアクセスします。
5. 📊 管理者は管理ダッシュボードからインスタンス健全性とクラスタ資源を監視します。

## 🏗 Architecture

```text
Browser
  -> ClawManager Frontend (React + Vite)
  -> ClawManager Backend (Go + Gin)
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

### High-Level Design

- フロントエンド: React 19 + TypeScript + Tailwind CSS
- バックエンド: Go + Gin + upper/db + MySQL
- ランタイム: Kubernetes
- アクセス層: WebSocket 転送対応の認証付きリバースプロキシ
- データ層: 業務データ用 MySQL、永続ストレージ用 PVC

## 🗂 Project Structure

```text
ClawManager/
├── backend/            # Go バックエンド API
├── frontend/           # React フロントエンド
├── deployments/        # コンテナと Kubernetes デプロイ設定
├── dev_docs/           # 設計・実装ドキュメント
├── scripts/            # 補助スクリプト
├── TASK_BREAKDOWN.md   # 詳細タスク分解
└── dev_progress.md     # 開発進捗記録
```

## 💻 Tech Stack

### Backend

- Go 1.21+
- Gin
- upper/db
- MySQL 8.0+
- JWT 認証

### Frontend

- React 19
- TypeScript 5.9
- Vite 7
- Tailwind CSS 4
- React Router

### Infrastructure

- Kubernetes
- Docker
- Nginx

## ☸️ Kubernetes Prerequisites

ClawManager は Kubernetes-first のプロジェクトです。管理対象ノードが Kubernetes クラスタに参加してはじめて、インスタンススケジューリング、資源確認、統合運用が可能になります。

ClawManager を導入する前に、利用可能な Kubernetes 環境を準備し、`kubectl` でアクセスできることを確認してください：

```bash
kubectl get nodes
```

### Linux セットアップ例

`k3s` を使う場合：

```bash
curl -sfL https://get.k3s.io | sh -
sudo kubectl get nodes
```

`microk8s` を使う場合：

```bash
sudo snap install microk8s --classic
sudo microk8s status --wait-ready
sudo microk8s kubectl get nodes
```

### Kubernetes 基本コマンド

```bash
kubectl get nodes
kubectl get pods -A
kubectl get pvc -A
kubectl cluster-info
```

### 最低推奨構成

- Kubernetes ノード 1 台
- 4 CPU
- 8 GB RAM
- 20+ GB の空きディスク

複数のデスクトップインスタンスを同時に動かす場合は、より多くの CPU、メモリ、ストレージを割り当ててください。

## 📦 Installation

インストール前に以下を確認してください：

- Kubernetes が利用可能
- `kubectl get nodes` が正常に動作する

同梱の Kubernetes YAML をそのまま適用します：

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## ⚡ Quick Start

### Default Accounts

- 既定の管理者アカウント: `admin / admin123`
- インポートされた管理者ユーザーの既定パスワード: `admin123`
- インポートされた一般ユーザーの既定パスワード: `user123`

### First Login

1. 👨‍💼 管理者としてログインします。
2. 👥 ユーザーを作成またはインポートし、quota を割り当てます。
3. 🧩 必要に応じてシステム設定でランタイムイメージカードを構成します。
4. 🖥 一般ユーザーとしてログインし、インスタンスを作成します。
5. 🔗 Portal View または Desktop Access からデスクトップにアクセスします。

## ⚙️ Configuration

ClawManager は明確なセキュリティモデルに従います：

- インスタンスサービスは Kubernetes 内部ネットワークを使用します
- デスクトップアクセスは ClawManager バックエンドの認証付きプロキシを経由します
- backend はクラスタ内に配置するのが最適です
- ランタイムイメージはシステム設定から集中管理できます
- 管理対象ノードは同一 Kubernetes クラスタに属している必要があります

主なバックエンド環境変数：

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

フロントエンド開発モードでは、Vite により `/api` がバックエンドへプロキシされます。

### CSV Import Template

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

注意点：

- `Email` は任意
- `Max GPU Count (optional)` は任意
- それ以外の列は必須
- quota 値はクラスタの容量計画と整合している必要があります

## 📄 License

本プロジェクトは MIT License の下で公開されています。

## ❤️ Open Source

機能、ドキュメント、テスト改善を含む issue と pull request を歓迎します。
