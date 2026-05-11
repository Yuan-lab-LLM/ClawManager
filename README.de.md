# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  ClawManager ist eine Kubernetes-native Control Plane fuer die Verwaltung von AI-Agent-Instanzen mit kontrolliertem AI-Zugriff, Runtime-Orchestrierung und wiederverwendbaren Ressourcen ueber mehrere Agent-Runtimes hinweg.
</p>

<p align="center">
  <strong>Sprachen:</strong>
  <a href="./README.md">English</a> |
  <a href="./README.zh-CN.md">简体中文</a> |
  <a href="./README.ja.md">日本語</a> |
  <a href="./README.ko.md">한국어</a> |
  Deutsch
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ClawManager-Control%20Plane-e25544?style=for-the-badge" alt="ClawManager Control Plane" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
</p>

<p align="center">
  <a href="#product-tour">Produktueberblick</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#runtime-integrations">Runtime-Integrationen</a> |
  <a href="#resource-management">Ressourcenverwaltung</a> |
  <a href="#get-started">Erste Schritte</a>
</p>

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
    <img src="https://img.shields.io/github/stars/Yuan-lab-LLM/ClawManager?style=for-the-badge&logo=github&label=Star%20ClawManager" alt="Star ClawManager on GitHub" />
  </a>
</p>

<h2 align="center">ClawManager in 60 Sekunden</h2>

<p align="center">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-launch-60s-hd.gif" alt="ClawManager Produktdemo" width="100%" />
</p>

<p align="center">
  Ein schneller Blick auf Agent-Provisionierung, Skill-Verwaltung und -Scanning sowie AI-Gateway-Governance.
</p>

## Neuigkeiten

Wichtige aktuelle Produkt- und Dokumentations-Updates.

- [2026-04-29] Hermes-Runtime-Integration hinzugefuegt, inklusive Webtop-basierter Instanzbereitstellung, Agent-Control-Plane-Registrierung, AI-Gateway-Injection, channel- und skill-Bootstrap sowie `.hermes` Import/Export. Siehe [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md).
- [2026-04-08] Skill-Verwaltung und Skill-Scanning wurden der Plattform hinzugefuegt. Details siehe [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52).
- [2026-03-26] Die AI-Gateway-Dokumentation wurde erweitert und deckt nun Modell-Governance, Audit und Trace, Kostenrechnung sowie Risikokontrolle genauer ab. Siehe [AI Gateway Guide](./docs/aigateway.md).
- [2026-03-20] ClawManager hat sich zu einer breiteren Control Plane fuer AI-Agent-Workspaces entwickelt, mit staerkerer Runtime-Steuerung, wiederverwendbaren Ressourcen und Security-Scanning-Workflows.

> Wenn ClawManager fuer dein Team nuetzlich ist, gib dem Projekt gerne einen Star, damit mehr Nutzer und Entwickler es entdecken.

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>

## WeChat-Community-Gruppe

Tritt der ClawManager Open-Source-Community auf WeChat bei, um Produkt-Updates zu verfolgen, Nutzungserfahrungen auszutauschen und mit Mitwirkenden ins Gespraech zu kommen.

<p align="center">
  <img src="./docs/main/clawmanager_group_chat.jpg" alt="QR-Code zur ClawManager WeChat-Gruppe" width="300" />
</p>

<a id="product-tour"></a>
## Produktueberblick

ClawManager bringt den Betrieb von AI-Agent-Instanzen auf Kubernetes und legt darauf drei hoeherwertige Control Planes. Teams koennen damit AI-Zugriff steuern, Runtime-Verhalten ueber Agents orchestrieren und Workspace-Faehigkeiten ueber scanbare und wiederverwendbare channel- und skill-Ressourcen bereitstellen.

Es eignet sich besonders fuer:

- Plattformteams, die AI-Agent-Instanzen fuer mehrere Nutzer betreiben
- Betriebsteams, die Runtime-Sichtbarkeit, Command-Dispatch und Desired-State-Kontrolle benoetigen
- Entwicklungsteams, die Agent-Workspaces ueber wiederverwendbare Ressourcen statt ueber manuelle Konfiguration bereitstellen wollen

<a id="runtime-integrations"></a>
## Runtime-Integrationen

ClawManager unterstuetzt derzeit die folgenden verwalteten Runtimes:

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`: die standardmaessige OpenClaw-artige Workspace-Runtime fuer von ClawManager verwaltete Desktop-Instanzen
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`: eine Webtop-basierte Runtime-Integration mit persistentem `.hermes`-Workspace und eingebettetem Hermes agent

Runtime-Vorschau:

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![openclaw](./docs/images/openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![hermes](./docs/images/hermes.png)

Runtime-Autoren koennen dem [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md), dem [Generic Runtime Agent Integration Guide](./docs/runtime-agent-integration-guide.md) und der [Skill Content MD5 Spec](./docs/skill-content-md5-spec.md) folgen, um kompatible Agents zu bauen.

<a id="get-started"></a>
## Erste Schritte

ClawManager bietet jetzt klarere Einstiegspfade sowohl fuer Standard-Kubernetes als auch fuer leichtere Cluster-Setups. Zum Evaluieren der Plattform ist es am sinnvollsten, zuerst den passenden Deployment-Pfad fuer die eigene Umgebung zu waehlen und danach dem First-Use-Flow zu folgen.

- Standard-Kubernetes-Deployment: [deployments/k8s/clawmanager.yaml](./deployments/k8s/clawmanager.yaml)
- K3s / leichtgewichtiges Deployment: [deployments/k3s/clawmanager.yaml](./deployments/k3s/clawmanager.yaml)
- First-Login- und Schnellstart-Ablauf: [Benutzerhandbuch](./docs/use_guide_de.md)
- Deployment-Hinweise und Architekturkontext: [Deployment Guide (English)](./docs/deployment.md)

## Drei Control Planes

<a id="ai-gateway"></a>
### AI Gateway

AI Gateway ist die Governance-Ebene fuer Modellzugriffe in ClawManager. Es stellt verwalteten Agent-Runtimes einen einheitlichen OpenAI-kompatiblen Einstiegspunkt bereit und legt Richtlinien-, Audit- und Kostenkontrollen ueber die Upstream-Provider.

- Einheitlicher Einstiegspunkt fuer Modell-Traffic
- Sichere Modell-Routing-Logik und policy-gesteuerte Modellauswahl
- End-to-End-Audit- und Trace-Aufzeichnungen
- Integrierte Kostenrechnung und Nutzungsanalyse
- Regeln fuer Risikokontrolle mit Block- oder Umleitungslogik

Siehe [AI Gateway Guide (English)](./docs/aigateway.md).

<a id="agent-control-plane"></a>
### Agent Control Plane

Agent Control Plane ist die Runtime-Orchestrierungsschicht fuer verwaltete AI-Agent-Instanzen. Jede Instanz wird damit zu einer verwalteten Runtime, die sich registrieren, Status melden, Commands empfangen und sich am Desired State der Plattform ausrichten kann.

- Agent-Registrierung mit sicherem Bootstrap und Session-Lifecycle
- Heartbeat-basierte Runtime-Status- und Health-Reports
- Desired-State-Synchronisierung zwischen Control Plane und Instanz
- Command-Dispatch fuer Start, Stop, Konfigurationsanwendung, Health Checks und Skill-Operationen
- Sichtbarkeit pro Instanz fuer Agent-Status, channel, skill und Command-Historie

Siehe [Agent Control Plane Guide (English)](./docs/agent-control-plane.md).

<a id="resource-management"></a>
### Ressourcenverwaltung

Ressourcenverwaltung ist die wiederverwendbare Asset-Schicht fuer AI-Agent-Workspaces. Teams koennen channel und skill vorbereiten, zu bundles zusammensetzen, in Instanzen injizieren und Security-Reviews direkt in diesen Ablauf integrieren.

- `Channel`-Verwaltung fuer Workspace-Konnektivitaet und Integrationsvorlagen
- `Skill`-Verwaltung fuer wiederverwendbare Faehigkeitspakete
- `Skill Scanner`-Workflows fuer Risikoanalyse und Scan-Jobs
- Bundle-basierte Ressourcenzusammenstellung fuer reproduzierbare Setups
- Injection-Snapshots zur Nachverfolgung der tatsaechlich angewendeten Inhalte

Siehe [Resource Management Guide (English)](./docs/resource-management.md) und [Security / Skill Scanner Guide (English)](./docs/security-skill-scanner.md).

## Produktgalerie

ClawManager ist so gestaltet, dass Administration, Zugriff und AI-Governance nicht wie getrennte Werkzeuge wirken, sondern wie eine zusammenhaengende Produkterfahrung.

### Admin Console

Die Admin-Konsole vereint Nutzer, Quotas, Runtime-Operationen, Security-Kontrollen und plattformweite Richtlinien in einer Oberflaeche. Sie ist die zentrale Arbeitsflaeche fuer Teams, die AI-Agent-Infrastruktur im grossen Massstab betreiben.

<p align="center">
  <img src="./docs/main/admin.png" alt="ClawManager Admin Console" width="100%" />
</p>

### Portal Access

Das Portal bietet Nutzern einen klaren Einstiegspunkt in ihre Workspaces. Der Zugriff erfolgt browserbasiert, waehrend Runtime-Zustand und Plattformsicht erhalten bleiben, ohne dass Infrastrukturdetails direkt exponiert werden.

<p align="center">
  <img src="./docs/main/portal.png" alt="ClawManager Portal Access" width="100%" />
</p>

### AI Gateway

AI Gateway integriert Modell-Governance direkt in die Workspace-Erfahrung. Audit-Trails, Kostentransparenz und risikobasiertes Routing machen AI-Nutzung zu einem Teil der Plattform statt zu einer losen Einzelintegration.

<p align="center">
  <img src="./docs/main/aigateway.png" alt="ClawManager AI Gateway" width="100%" />
</p>

## So funktioniert es

1. Administratoren definieren Governance-Richtlinien und wiederverwendbare Ressourcen.
2. Nutzer erstellen oder betreten verwaltete AI-Agent-Workspaces auf Kubernetes.
3. Agents verbinden sich mit der Control Plane und melden Runtime-Zustaende.
4. Channel, skill und bundle werden kompiliert und auf Instanzen angewendet.
5. AI-Traffic fliesst ueber das AI Gateway und erhaelt Audit-, Risiko- und Kostenkontrollen.

## Entwicklerueberblick

ClawManager ist eine Kubernetes-native Plattform mit React-Frontend, Go-Backend, MySQL fuer Zustandsdaten sowie Integrationen wie `skill-scanner` und Object Storage. Die Codebasis ist nach Produktsubsystemen organisiert, daher ist der schnellste Einstieg, mit dem passenden Guide zu beginnen und danach in den Code zu gehen.

- Frontend fuer Admin- und Nutzeroberflaechen unter `frontend/`
- Backend-Services, Handler, Repositorys und Migrationen unter `backend/`
- Deployment-Assets unter `deployments/`
- Produktdokumentation und Medien unter `docs/`

Siehe [Developer Guide (English)](./docs/developer-guide.md).

## Dokumentation

- [Benutzerhandbuch](./docs/use_guide_de.md)
- [Deployment Guide (English)](./docs/deployment.md)
- [Admin and User Guide (English)](./docs/admin-user-guide.md)
- [Agent Control Plane Guide (English)](./docs/agent-control-plane.md)
- [AI Gateway Guide (English)](./docs/aigateway.md)
- [Security / Skill Scanner Guide (English)](./docs/security-skill-scanner.md)
- [Resource Management Guide (English)](./docs/resource-management.md)
- [Hermes Runtime Guide](./docs/hermes-runtime-agent-development.md)
- [Generic Runtime Agent Integration Guide](./docs/runtime-agent-integration-guide.md)
- [Skill Content MD5 Spec](./docs/skill-content-md5-spec.md)
- [Developer Guide (English)](./docs/developer-guide.md)

## Lizenz

Dieses Projekt steht unter der MIT License.

## Open Source

Issues und Pull Requests sind willkommen.

## Star History

<a href="https://www.star-history.com/?repos=Yuan-lab-LLM%2FClawManager&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=Yuan-lab-LLM/ClawManager&type=date&legend=top-left" />
 </picture>
</a>
