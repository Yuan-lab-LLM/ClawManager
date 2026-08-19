[← README に戻る](../README.ja.md)

# リソース管理ガイド

リソース管理は、再利用可能な OpenClaw 起動設定を準備するユーザー向け画面です。管理者向けの **Security Protection** とは別機能であり、リソース管理は設定と配布、Security Protection はリスク監視とガバナンスを担当します。

![OpenClaw リソース管理](./main/resource-management-current.png)

## 3 つのタブ

- **リソース**: 個別定義の検索と管理。
- **リソースパック**: 複数のリソースを再利用可能な起動構成にまとめる。
- **注入記録**: インスタンス作成時にコンパイルされ、再起動時に再利用されるスナップショットを確認する。

## リソース種別

- **Channel**: 通信設定を作成、編集、有効/無効化、複製、削除できます。Telegram、DingTalk、WeCom、Slack、Feishu などの対応テンプレートにはフォームと JSON 編集があります。
- **Skill**: 1 つ以上の ZIP をアップロードし、競合を解決し、ダウンロードまたは削除できます。Catalog、Ownership、Version、Publish、後からの Install は **Skill Hub** が担当します。
- **Agent**: 予約された種別として表示されますが、現在この画面では設定できません。
- **Scheduled Task**: 簡易フォームまたは高度な JSON で OpenClaw Job を作成・編集します。cron、間隔、単発実行、announce、webhook、配信なしに対応します。

Session Template と Log Policy は内部モデルに存在しますが、この画面では意図的に非表示です。

## リソースパックと注入記録

リソースパックは、有効なリソースと対象 Skill をまとめます。作成、編集、有効/無効化、複製、削除が可能で、同じ基準構成を複数インスタンスへ届ける場合に使います。

注入記録は読み取り専用で、Snapshot ID、配布モード、リソース数、環境変数数、状態、作成時刻を表示します。「何が配布されたか」を確認する記録であり、セキュリティイベントではありません。

## 他機能との境界

- **Skill Hub** は OpenClaw、Hermes、OpenCode、DeepSeek Harness 向け Skill の Catalog、Version、Publish、Install を管理します。
- **インスタンス作成**では Runtime に応じて Archive、Resource Pack、個別 Resource、Skill を選択します。
- **Security Protection** は Runtime 防御、分離、Policy、緊急対応、Audit のための独立した管理者機能です。Skill Scanner はその一つの Scenario であり、リソース管理のタブではありません。

[Skill Hub](./skill-hub-guide_ja.md)、[Security Protection](./security-platform_ja.md)、[ユーザーガイド](./use_guide_ja.md) も参照してください。
