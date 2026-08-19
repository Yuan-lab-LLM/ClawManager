[← README に戻る](../README.ja.md)

# Security Protection Platform ガイド

**Security Protection** は管理者向けの独立したワークスペースであり、ユーザー向けリソース管理の一部ではありません。Runtime 防御、Host/Container 保護、Component Trust、Identity、Policy、Collaboration Governance、緊急対応、Security Event を一つの画面にまとめます。

![ClawManager Security Protection](./main/security-protection-current.png)

## Overview と操作

4 つの指標は、本日の防御 Hit、過去 24 時間の High Severity、Block/Deny Event、影響を受けた Agent Instance を表示します。Alert は自動更新され、最新 10 件には時刻、Source、Scenario、Evidence、Target、Severity が表示されます。

- **Pod Live Aegis Configuration**: Runtime Security 設定と Dispatch を開きます。
- **Report Export**: 読み込まれた Alert を JSON Lines で保存します。
- **Emergency Circuit Breaker**: 理由と確認を求めて緊急状態を配布し、有効中は実行者、時刻、理由と解除操作を表示します。

Live 設定や Circuit Breaker を操作する前に、対象範囲と影響を確認してください。

## KSecure モデル

UI は **7 Risk Surface、15 Defense Scenario、4 Defense Layer** として表示し、Layer View と Ring View を切り替えられます。

- **Runtime Layer**: Input、State/Memory、Decision/Tool Call、Output、Asset Protection、Human Approval。
- **Host Layer**: Host Hardening と Container Isolation。
- **Audit Layer**: Skill Scanner と制御された Private Egress Exception。
- **Control Layer**: Outbound/Identity Governance、Policy Template、Circuit Breaker、Full-chain Audit、Team Collaboration、AI Gateway Quota。

各カードは Scenario ページへの入口です。表示されていても、すべての Backend Enforcement が有効とは限りません。利用可能な操作は配備済み Security Service と Runtime Agent に依存します。

## 運用と境界

Event と対象を特定し、対応 Scenario の設定または Evidence を確認し、影響が最小の対処を選びます。Circuit Breaker は中断が必要で範囲を理解している場合だけ使用してください。

Resource Management は Channel、Skill、Scheduled Task、Resource Pack、Injection Record を扱います。Skill Scanner は Security Protection 内の一つの Scenario です。ユーザーは Skill Hub で Upload、Scan Status、Report を確認し、管理者はここで Scanner Health、Failed Job、Model/Meta LLM、Quick/Deep Policy、Security Event を確認します。Scan 完了は自動承認ではありません。本機能は Kubernetes Hardening、Network Policy、Credential、Backup、組織の Incident Response を代替しません。

[Skill Hub](./skill-hub-guide_ja.md)、[リソース管理](./resource-management_ja.md)、[AI Gateway](./aigateway_ja.md)、[ユーザーマニュアル](./use_guide_ja.md) も参照してください。
