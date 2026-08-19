[← README に戻る](../README.ja.md)

# AI Gateway ユーザーガイド

AI Gateway は OpenClaw、Hermes、OpenCode、DeepSeek Harness、Team、Platform Function の管理対象 Model Access Layer です。

## Instance 作成前

**AI Gateway → Models** で通常 Model を 1 つ以上追加、有効化し Health Test を行います。Security Model は Risk Rule が Sensitive Request を別経路に Route する場合だけ必要です。Active Model がなければ Model を使う Instance や Custom Team は作成できません。

## 5 つの領域

- **Models**: Provider URL、Model、Credential、Protocol、Price、Health、Enable、Security Role、Thinking。
- **AI Audit**: Trace、Routing、Provider Response、Policy Hit、Latency、Error。
- **Costs**: Token 見積りと内部 Accounting。
- **Session Usage**: User、Runtime、Instance、Session 別の利用量。
- **Risk Rules**: 順序付き Allow、Block、Secure Route。

Managed Thinking は ClawManager が確実に制御できる組合せの永続設定です。Latency と Reasoning Token が増える場合がありますが非公開の思考は表示しません。Model 側で Off の場合、Runtime は勝手に On にできません。

## Protocol と Routing

OpenAI Chat Completions、OpenAI Responses、Anthropic Messages に対応します。Upstream Provider に合う Protocol を選び、Streaming と Tool Call を本番前に確認します。Risk Rule は順序どおりに Block、Active Security Model への Route、または通常 Model 継続を決定します。

## Session Usage

期間、User、Runtime、Instance、Session で絞り込み、Runtime/Provider が報告した Input、Output、Cached、Reasoning Token と設定価格による見積りを比較します。Request Routing、Error、Policy の根拠は **AI Audit** で確認します。

Session Usage は観測画面であり、Conversation Editor や Provider の最終請求ではありません。古い、中断、未対応 Session は不完全で、Retry や Token 分類差で合計が異なる場合があります。

## トラブルシュート

| 症状 | 確認 |
|---|---|
| Creation に Model がない | 通常 Model が Active/Healthy。 |
| Thinking が無効 | Provider/Model が管理対象外。 |
| Session Usage が空 | 期間、Filter、Runtime Report。 |
| Cost がない | Model の Input/Output Price。 |
| Block/Route が予想外 | AI Audit の Risk Rule と順序。 |

[ユーザーマニュアル](./use_guide_ja.md) と [Security Protection](./security-platform_ja.md) も参照してください。
