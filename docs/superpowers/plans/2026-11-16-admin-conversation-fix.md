# Admin conversation counting correction

**Goal:** Correct zero conversations for ownerless instances, deploy CM app to54, and prove the displayed total/hourly curve against real records.

**Root cause:** All36test instances have blank owner. The dashboard incorrectly reads only owner-bound question_v4 growth records; that source contains no question rows. Gateway chat_messages already contains8observed user-turn traces today from Hermes/DSH, despite19model invocations. Current-turn persistence strips older user turns; tool rounds and retries reuse trace IDs.

**Approach:** Reuse durable platform chat turn records, independently of owner/growth epoch. A counted turn has a first-sequence user message and an assistant message linked to a completed LLM invocation with matching user/instance/session/trace. Group by all four attribution keys and earliest root-message time; do not count transcript rows or model attempts. Use one bounded query for daily total and hourly values. Coverage remains partial for traffic outside the CM gateway; do not add native/gateway totals with incompatible identities.

- [x] Add regression proving owner data failure cannot suppress platform turns; unavailable chat reads stay null. Prove grouping/failure/tool/replay/time/scoping behavior with executable SQL fixture and read-only MySQL checks.
- [x] Replace owner/growth query with platform query; update tooltip to accurate source semantics. Preserve all graph sizing and other UI changes.
- [x] Run targeted Go tests and real SELECT-only MySQL on54, local TypeScript/source browser. Review correction.
- [x] Freeze source; build binary/frontend/image on54only, update only CM app pinned digest. Runtime Deployments/helper security remain intact.
- [x] Verify nonzero deployed total and nonzero hourly curve equal independent raw record grouping; repeat UI/native-origin and hash checks. Update HTML report.

No synthetic conversations, owner rewrites, billing changes, growth-credit changes, Runtime changes or database backfill.

Final metric contract: recorded gateway user turns with at least one successful model response, including tool intermediate responses. Same-trace retries deduplicate; final task completion is not implied. All5locale labels/tooltips and API note reflect this. Review Important resolved and approved. Go targeted tests +SQLite grouping fixture PASS; realSELECT-only MySQL daily8/hourly8 PASS; sourcebrowser30locale/viewport combinations PASS; TypeScript appconfig noEmit PASS.

Deployed source975f7fef4e95, CM image18c6467429512ea242518209e48ac44ef36d5985e5aa75c2c0a6a05358baca44, versionv2026.11.16-admin-dialogue, ready1/1. Live12/12checksPASS: actual8turns/hourly8/nonzerocurve,6screensandlanguage/API/auth/nativeOpenCode/DSH. Source/index/backend hashes verified; otherDeployment specsUIDgeneration unchanged;128browserNodePorts8444/readyEndpoint verified. Updated docs/reports/2026-11-16-admin-overview.html with finalsource/deploy/countproof. No instance lifecycle, owner, billing, XP, schema or Runtime mutations.
