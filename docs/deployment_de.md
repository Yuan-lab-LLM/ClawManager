[← Zurueck zur README](../README.de.md)

# Deployment Guide

ClawManager ist Kubernetes-nativ. Zuerst Distribution und danach ein zum Storage passendes Profil waehlen.

## Profile

| Umgebung | Manifest | Storage |
|---|---|---|
| k3s Single Node | [`k3s/single-node`](../deployments/k3s/single-node/clawmanager.yaml) | HostPath auf markiertem Node |
| k3s Cluster | [`k3s/cluster`](../deployments/k3s/cluster/clawmanager.yaml) | CSI RWO + RWX |
| Kubernetes Single Node | [`k8s/single-node`](../deployments/k8s/single-node/clawmanager.yaml) | HostPath auf markiertem Node |
| Kubernetes Cluster | [`k8s/cluster`](../deployments/k8s/cluster/clawmanager.yaml) | CSI RWO + RWX |

Nur ein vollstaendiges Profil verwenden. Longhorn-Namen sind Beispiele; kompatible StorageClasses koennen sie ersetzen. Keine Manifeste mischen und kein temporaeres `/tmp` HostPath fuer Multi-Node verwenden.

## Inhalt und Ablauf

Die Profile starten ClawManager, MySQL, MinIO, Skill Scanner, Team Redis, Workspace Services sowie Lite-Pools fuer OpenClaw, Hermes, OpenCode und DeepSeek Harness. Lite-Instanzen teilen einen Runtime Pod.

1. Secrets, Images, StorageClass und externen Zugang pruefen.
2. Single Node korrekt labeln oder RWO/RWX CSI im Cluster bestaetigen.
3. Ein Profil anwenden und auf Ready Pods sowie Bound PVCs warten.
4. Normales Modell konfigurieren, AI Gateway und Security oeffnen.
5. Je freigegebenem Runtime eine Testinstanz erstellen.

Neues MySQL wird ueber `clawmanager-mysql-init` initialisiert; bestehende Volumes wiederholen First-Start-Skripte nicht. Dauerhafte Daten duerfen nicht auf `emptyDir` liegen.

## DeepSeek Harness Runtime

- Lite startet einen isolierten `dsh web`-Prozess im gemeinsamen Pool `deepseek-harness-runtime`; das persistente Home liegt unter `<workspace>/home/.dsh`.
- Pro verwendet ein dediziertes Webtop Deployment auf Port `3001`; `dsh web` lauscht intern auf Loopback-Port `3080`, das persistente Home ist `/config/.dsh`.
- Beide Modi erhalten AI-Gateway-URL, Instanz-Credential und Modellliste von ClawManager und integrieren Skills sowie Workspace-Dateien.
- Lite benoetigt ein eigenes Origin-Template, zum Beispiel `CLAWMANAGER_DEEPSEEK_HARNESS_PUBLIC_URL_TEMPLATE=https://deepseek-harness-{instance_id}.clawmanager.test:39443/`; `{instance_id}` ist erforderlich.

Die Images werden im [`deepseek-harness/`-Verzeichnis von AgentsRuntime](https://github.com/Iamlovingit/AgentsRuntime/tree/main/deepseek-harness) gepflegt. Offline-Installationen benoetigen Wildcard-DNS und ein passendes Zertifikat fuer die Lite-Origins.

## Storage und ARM64

Single-Node HostPath braucht festen Node und Affinity. Multi-Node benoetigt echtes RWO/RWX CSI; `local-path` ist kein clusterweites RWX. Clusterprofile sollen keinen impliziten HostPath Fallback verwenden.

ClawManager und Skill Scanner bieten `linux/arm64`, aber auch MySQL, Redis, MinIO/Workspace und alle Runtime Images muessen ARM64 unterstuetzen. In Mixed-Architecture-Clustern kompatible Tags mit Node Selector/Affinity einsetzen. SSD, genug RAM und feste Tags nutzen; dieselben PVC-, Desktop-, Modell- und Skilltests wie auf amd64 ausfuehren.

## Abnahme

Pods/PVCs, Webzugriff, Modell, Security, AI Gateway und alle benoetigten Runtimes pruefen. Bei PVC Pending StorageClass, PVC, Pods, Events und Describe sammeln, bevor Daten entfernt werden. Siehe [Benutzerhandbuch](./use_guide_de.md).
