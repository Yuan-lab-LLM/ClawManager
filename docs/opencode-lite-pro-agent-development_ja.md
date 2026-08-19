[← README に戻る](../README.ja.md)

# OpenCode ワークスペースガイド

OpenCode は ClawManager の管理対象 Coding Workspace です。公式 OpenCode を使用し、AI Gateway 経由で Model にアクセスします。

## Lite と Pro

| Mode | 形態 | 境界 |
|---|---|---|
| Lite | 共有 Runtime Pod 内の隔離 Process/Workspace | Instance ごとの Pod はありません |
| Pro | Dedicated Desktop Workload | Default Image の Save は既存 Instance を自動置換しません |

両 Mode は選択した Storage Profile に従い Workspace を永続化します。Lite Portal は ClawManager が適応し、Pro は Dedicated Desktop から OpenCode を開きます。

## 作成前

管理者は対応 OpenCode Image と通常 AI Gateway Model を 1 つ以上有効化します。Lite Pool は Healthy である必要があり、New Image の Save 後は Lite Rolling Upgrade も必要です。User の Resource Quota も確認します。

OpenCode は管理対象 AI Gateway Provider 設定を受け取ります。管理者の設計なしに OpenCode から別 Provider Key を追加しないでください。

## 使用

**My Instances → Create** で OpenCode と Lite/Pro を選択し、Image、Resource、Environment、表示された Start Resource を設定します。Instance Page で Lifecycle、Terminal/Desktop、Files を利用します。

- Start/Stop/Restart/Delete は ClawManager から実行します。
- Project は一時 Directory ではなく表示された Workspace に保存します。
- Storage 対応範囲で File Panel の Upload/Download/Edit/Delete を使います。
- Stream Profile と Environment の変更は通常 Apply/Restart が必要です。
- Share Link には期限、Credential、必要最小限の Workspace Permission を設定します。

## AI Gateway と Skill

Model Error は Instance State、Model Health、Protocol、AI Audit の順に確認します。通常利用に Security Model は不要です。

Skill Hub は OpenClaw、Hermes、OpenCode、DeepSeek Harness 共通機能です。OpenCode Lite は `{workspace}/home/.opencode/skills`、managed HostPath Pro は `/config/workspace/.opencode/skills` を使います。Creation に選択がない場合は後から Install し Skill Management で確認します。Non-HostPath Pro は Runtime Agent Command が必要です。

## 境界とトラブルシュート

- OpenClaw Config Plan、Archive、Team Persona は継承しません。
- Standard Team では現在 OpenCode を Leader/Worker にしません。
- Scheduled Task は UI に表示される場合だけ利用可能と判断します。
- Old Lite Image: Save 後に Rolling Upgrade。
- Portal Failure: Instance/Pool Health と Event。
- File Loss: Workspace Path、PVC、Storage Profile。
- Skill Failure: Materialization と Runtime Agent Capability。

Create/Start/Stop/Restart、Portal/Desktop、Streaming/Tool、Persistence、Share Link、利用する Skill Flow を受入確認します。[ユーザーマニュアル](./use_guide_ja.md)、[AI Gateway](./aigateway_ja.md)、[Skill Hub](./skill-hub-guide_ja.md) も参照してください。
