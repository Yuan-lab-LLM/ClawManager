# OpenCode / DSH browser-origin entry recovery — 2026-11-12

## Cause

The app was configured with `CLAWMANAGER_BROWSER_ORIGIN=https://10.130.81.54:30443` and `CLAWMANAGER_BROWSER_PORT_RANGE=31600-31727`, but the test cluster had no `clawmanager-browser-origins` Service. The allocated OpenCode origin31601 rejected TCP connections. The frontend certificate probe treats fetch failures as certificate confirmation requirements, so the displayed confirmation did not identify the missing network entry.

The missing Service was separate from the working manager frontend Service30443. Restoring the documented origin-port exposure fixes the actual failed connection; no certificate checks, instance authentication or browser-origin separation were removed.

## Applied repair

Created `clawmanager-system/clawmanager-browser-origins`, selector `app=clawmanager-app`, NodePorts31600–31727, each forwarding TCP to the existing TLS nginx8444 listener. The full128-port mapping and ready EndpointSlice were verified. A server-side dry run of the checked-in manifest reports unchanged.

Persistent manifest: [server54 browser origins](../../../deployments/k8s/sites/server54/browser-origins.yaml). Updated [operations documentation](../../browser-origin-ports.md) explains the required Service, reinstallation steps and verification. AGENTS.md now requires native-entry checks when browser-origin mode is enabled. This is an explicit manifest application and release-check requirement, not an automated installer.

## Verification

- Before repair:31601 connection refused; no Service exposed the configured pool.
- After repair: native probes return204 on OpenCode31601 and DSH31602. Their TLS certificate fingerprint matches the existing manager30443 certificate.
- Fresh Chrome context with default TLS verification (`ignoreHTTPSErrors=false`); confirmed the main30443 hostname once through Chrome's actual certificate interstitial. Did not confirm either child origin and did not install a CA.
- OpenCode instance50 (`lite-opencode-010`) loads its actual native interface and composer on31601. DSH instance60 (`lite-dsh-010`) loads its actual application on31602. Both certificate probes204, no child confirmation heading, no native failed requests. Both screenshots inspected.
- Switching test pages originally recorded the previous OpenCode page's intentionally cancelled streaming requests; isolated each instance in its own page and reran successfully.
- Unauthenticated manager API attempts against31601 and31602 return403; unassigned31727 returns404. Dedicated ports do not expose manager APIs.
- Application image and generation remain `eb8102549c2a9a75d8aab7f7771ac1d94d24a07b521bc40fa4588904e17272e6`, generation19. No app/runtime image build, rollout, instance restart, workspace mutation or paid inference was needed.
- Independent read-only configuration review found no actionable issue.

Private operator evidence, access capabilities and screenshots remain under `/root/cm-browser-origins-20261112/`; no capabilities or private screenshots are committed. Existing browser TLS exceptions can expire or be cleared as documented; this repair removes the missing-port fault, not legitimate browser TLS validation.
