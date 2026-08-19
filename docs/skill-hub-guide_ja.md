[← README に戻る](../README.ja.md)

# Skill Hub ユーザーガイド

Skill Hub は OpenClaw、Hermes、OpenCode、DeepSeek Harness 共通の Version 管理 Skill Catalog です。インスタンス内の File を Scan、Publish、再 Install できる Asset に変換する基盤で、OpenCode 専用機能ではありません。

## View

- **Browse**: Published Skill の検索、Tag Filter、Author/Version/Scan/Risk 確認、対応 Instance への Install。
- **My Skills**: Upload または Instance から収録した Skill、Version、Tag、Publish/Unpublish、Download、Delete。
- **Admin**: Platform Skill と許可された Governance。Button は Ownership と Version State に依存します。
- **Instance Skill Management**: Installed、Hub-managed、Workspace-discovered Skill と実際の Version を確認します。

## Upload、Scan、Publish

1. My Skills で `SKILL.md` と必要 File を含む ZIP を 1 つ以上 Upload します。
2. ClawManager が Version を保存し Security Scan を開始します。一般的な Windows/CJK ZIP Filename を処理します。
3. Scan Status、Risk、Finding を確認します。Failed Version は修正用に残ります。
4. Version と Policy の条件を満たしたら Public Tag を設定し Publish します。
5. Unpublish は Browse から外すだけで Owner の History は削除しません。

Scan Completed は自動的な安全保証や承認ではありません。Package、Ownership、Runtime Compatibility、Platform Policy も判定に使われます。

## Install と確認

Skill Detail で Install を選び、対応 Instance と Version を確定します。その後 Instance の Skill Management を Refresh し、実際の Version を確認します。OpenClaw、Hermes、OpenCode、DeepSeek Harness に対応しますが、保存先と Reload は Runtime ごとに異なります。DeepSeek Harness は Lite で `home/.dsh/skills`、Pro で `.dsh/skills` を使用します。

## Instance から収録

Workspace Discovery は自動的に My Skills へ追加しません。内容と Source を確認し **Collect to library** を選び、Package/Scan 後に Publish を判断します。Drift がある場合は Installed Version を復元するか、現状を New Version として収録し、History を上書きしません。

## 境界とトラブルシュート

- 既存 YAML Frontmatter は書き換えないため、`name` と `description` を確認します。
- Capability Tag は検索用で、自動 Install ではありません。
- Publish が無効: Scan、Package、Ownership、Risk/Policy を確認します。
- Target Instance がない: Ownership、Runtime Support、Instance State を確認します。
- Install 後に見えない: Skill Management、Version/Path、必要な Runtime Reload を確認します。

[Resource Management](./resource-management_ja.md)、[Security Protection](./security-platform_ja.md)、[ユーザーマニュアル](./use_guide_ja.md) も参照してください。
