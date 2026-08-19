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
  <a href="https://discord.gg/9RwgbGJD5R">
    <img src="https://img.shields.io/badge/Discord-Join%20Us-5865F2?style=for-the-badge&logo=discord&logoColor=white" alt="ClawManager Discord-Community beitreten" />
  </a>
</p>

<p align="center">
  <a href="#product-tour">Produktueberblick</a> |
  <a href="#team-workspaces">Team Workspaces</a> |
  <a href="#ai-gateway">AI Gateway</a> |
  <a href="#agent-control-plane">Agent Control Plane</a> |
  <a href="#runtime-integrations">Runtime-Integrationen</a> |
  <a href="#resource-management">Ressourcenverwaltung</a> |
  <a href="#security-protection-platform">Security Protection</a> |
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

- [2026-08-19] Verwaltete OpenCode-Workspaces, eine aktualisierte Instanzansicht und Skill-Hub-Bereitstellung fuer OpenClaw, Hermes und OpenCode hinzugefuegt. Siehe [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_de.md).
- [2026-08-18] Team-Kollaboration um acht schreibgeschuetzte Vorlagen, benutzerdefinierte Teams aus natuerlicher Sprache, optionale Hermes Lite Worker, Live-Kanban, gemeinsame Artefakte und Member-Sessions erweitert.
- [2026-08-17] Modellgesteuertes Thinking, AI-Gateway Session Usage, bearbeitbare geplante Aufgaben und erweiterte Lite-Lifecycle- und Batch-Funktionen hinzugefuegt.
- [2026-08-16] DeepSeek Harness Lite und Pro mit isolierten Shared-Runtime-Pools, dedizierten Webtop-Desktops, AI-Gateway-Modellinjektion, Skills/Workspace-Integration und eigener Lite-Browser-Origin hinzugefuegt.
- [2026-07-07] Security Protection Platform (secplane) Frontend-Konsole hinzugefuegt — umfassende Sicherheitskonsole mit Runtime-Abwehr (Eingabe-/Zustands-/Entscheidungs-/Ausgabeoberflaeche, Asset-Schutz, menschliche Freigabe), Host-Haertung und Container-Isolierung, Outbound-Vertrauens-Governance, Richtlinien-Governance, Kill-Switch/Circuit-Breaker, Full-Chain-Audit, SecureClaw-Daten- und Komponentenvertrauens-Audit, Kollaborations-Governance und Eingabeerkennung. 4 Verteidigungsschichten in einer einheitlichen Admin-UI mit vollstaendiger i18n fuer 5 Sprachen.
- [2026-06-14] Lite-/Pro-Runtime-Modi und Rollout-Support hinzugefuegt: Lite-Instanzen laufen ueber gemeinsame Gateway-Runtime-Pools, waehrend Pro-Instanzen dedizierte Desktop-Deployments fuer staerkere Isolation behalten.
- [2026-05-18] Team-Workspace-MVP mit Einfuehrung und Vorschau hinzugefuegt, inklusive One-Click-Team-Erstellung, OpenClaw-Member-Orchestrierung, Redis-Team-Bus-Injection, Shared Storage, Member-Status, Task-Dispatch sowie Event- und Ergebnisansichten.
- [2026-04-29] Hermes-Runtime-Integration hinzugefuegt, inklusive Webtop-basierter Instanzbereitstellung, Agent-Control-Plane-Registrierung, AI-Gateway-Injection, channel- und skill-Bootstrap sowie `.hermes` Import/Export. Siehe [Benutzerhandbuch](./docs/use_guide_de.md#create-a-workspace).
- [2026-04-08] Skill-Verwaltung und Skill-Scanning wurden der Plattform hinzugefuegt. Details siehe [Merged PR #52](https://github.com/Yuan-lab-LLM/ClawManager/pull/52).
- [2026-03-26] Die AI-Gateway-Dokumentation wurde erweitert und deckt nun Modell-Governance, Audit und Trace, Kostenrechnung sowie Risikokontrolle genauer ab. Siehe [AI Gateway Guide](./docs/aigateway_de.md).
- [2026-03-20] ClawManager hat sich zu einer breiteren Control Plane fuer AI-Agent-Workspaces entwickelt, mit staerkerer Runtime-Steuerung, wiederverwendbaren Ressourcen und Security-Scanning-Workflows.

> Wenn ClawManager fuer dein Team nuetzlich ist, gib dem Projekt gerne einen Star, damit mehr Nutzer und Entwickler es entdecken.

<p align="center">
  <a href="https://github.com/Yuan-lab-LLM/ClawManager/stargazers">
<img src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager-Assets/main/gif/clawmanager-star.gif" alt="Star ClawManager on GitHub" width="100%" />
  </a>
</p>

## Community

Tritt der ClawManager Open-Source-Community auf WeChat oder Discord bei, um Produkt-Updates zu verfolgen, Nutzungserfahrungen auszutauschen und mit Mitwirkenden ins Gespraech zu kommen.

<table align="center">
  <tr>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_group_chat.jpg" alt="QR-Code zur ClawManager WeChat-Gruppe" height="300" />
      <br /><br />
      <strong>WeChat</strong>
      <br />
      QR-Code scannen, um der WeChat-Gruppe beizutreten
    </td>
    <td align="center" width="320" valign="top">
      <img src="./docs/main/clawmanager_discord.jpg" alt="QR-Code zur ClawManager Discord-Einladung" height="300" />
      <br /><br />
      <strong>Discord</strong>
      <br />
      <a href="https://discord.gg/9RwgbGJD5R">QR-Code scannen, um unserem Discord-Server beizutreten</a>
    </td>
  </tr>
</table>

<a id="product-tour"></a>
## Produktueberblick

ClawManager vereint den Betrieb, die Zusammenarbeit und die Governance von AI Agents in einem Kubernetes-nativen Produkt: verwaltete Runtimes, Teams, Modellzugriff, Ressourcen und Skill Hub sowie Plattformsicherheit.

Es eignet sich besonders fuer:

- Plattformteams, die AI-Agent-Instanzen fuer mehrere Nutzer betreiben
- Betriebsteams, die Runtime-Sichtbarkeit, Command-Dispatch und Desired-State-Kontrolle benoetigen
- Entwicklungsteams, die Agent-Workspaces ueber wiederverwendbare Ressourcen statt ueber manuelle Konfiguration bereitstellen wollen

<a id="runtime-integrations"></a>
## Runtime-Integrationen

ClawManager unterstuetzt derzeit die folgenden verwalteten Runtimes:

- <img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> `OpenClaw`: Lite-/Pro-Workspaces mit Sessions, Tools, geplanten Aufgaben und Team-Support
- <img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> `Hermes`: Lite-/Pro-Workspaces mit persistentem `.hermes`-Home, nativen Sessions und Team-Worker-Support
- <img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> `OpenCode`: verwaltete Coding-Workspaces mit AI Gateway, Desktop/Terminal und Dateien. Siehe [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_de.md).
- <img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> `DeepSeek Harness`: Lite-Pool- und Pro-Desktop-Workspaces mit AI-Gateway-Modellinjektion, Skills, Workspace-Dateien und isoliertem Browserzugriff

Runtime-Vorschau:

**<img src="frontend/public/openclaw.png" alt="OpenClaw icon" width="18" /> OpenClaw**

![OpenClaw Workspace](./docs/main/runtime-openclaw.png)

**<img src="frontend/public/hermes.png" alt="Hermes icon" width="18" /> Hermes**

![Hermes Workspace](./docs/main/runtime-hermes.png)

**<img src="frontend/public/opencode.png" alt="OpenCode icon" width="18" /> OpenCode**

![OpenCode Workspace](./docs/main/runtime-opencode.png)

**<img src="frontend/public/deepseek-harness.svg" alt="DeepSeek Harness icon" width="18" /> DeepSeek Harness**

![DeepSeek-Harness-Workspace](./docs/main/runtime-deepseek-harness.png)

<a id="get-started"></a>
## Erste Schritte

Waehle zuerst `k3s` oder `k8s` und danach das Storage-Profil fuer einen Einzelknoten oder einen Cluster.

- k3s Einzelknoten / HostPath: [Manifest](./deployments/k3s/single-node/clawmanager.yaml)
- k3s Cluster / CSI-RWX: [Manifest](./deployments/k3s/cluster/clawmanager.yaml)
- Kubernetes Einzelknoten / HostPath: [Manifest](./deployments/k8s/single-node/clawmanager.yaml)
- Kubernetes Cluster / CSI-RWX: [Manifest](./deployments/k8s/cluster/clawmanager.yaml)
- First-Login- und Schnellstart-Ablauf: [Benutzerhandbuch](./docs/use_guide_de.md)
- Deployment-Hinweise und Architekturkontext: [Deployment Guide](./docs/deployment_de.md)

## Zentrale Plattformfunktionen

### Runtime- und Instanzverwaltung

OpenClaw-, Hermes-, OpenCode- oder DeepSeek-Harness-Workspaces in Lite oder Pro erstellen und Images, Ressourcen, Lifecycle, Desktop, Dateien, Shell, Umgebungsvariablen, Archive, Share Links und Lite-Batch-Aktionen zentral verwalten.

<a id="ai-gateway"></a>
### AI Gateway

AI Gateway bietet fuenf Bereiche: Modelle, AI Audit, Kosten, Session Usage und Risikoregeln. Es unterstuetzt Chat Completions, OpenAI Responses und Anthropic Messages sowie verwaltetes Thinking fuer kompatible Modelle.

- Einheitlicher Einstiegspunkt fuer Modell-Traffic
- Sichere Modell-Routing-Logik und policy-gesteuerte Modellauswahl
- End-to-End-Audit- und Trace-Aufzeichnungen
- Integrierte Kostenrechnung und Nutzungsanalyse
- Regeln fuer Risikokontrolle mit Block- oder Umleitungslogik

Siehe [AI Gateway Guide](./docs/aigateway_de.md).

<a id="agent-control-plane"></a>
### Agent Control Plane

Agent Control Plane ist die Runtime-Orchestrierungsschicht fuer verwaltete AI-Agent-Instanzen. Jede Instanz wird damit zu einer verwalteten Runtime, die sich registrieren, Status melden, Commands empfangen und sich am Desired State der Plattform ausrichten kann.

- Agent-Registrierung mit sicherem Bootstrap und Session-Lifecycle
- Heartbeat-basierte Runtime-Status- und Health-Reports
- Desired-State-Synchronisierung zwischen Control Plane und Instanz
- Command-Dispatch fuer Start, Stop, Konfigurationsanwendung, Health Checks und Skill-Operationen
- Sichtbarkeit pro Instanz fuer Agent-Status, channel, skill und Command-Historie

Lifecycle, Status, Restart, Runtime Health und Adminbetrieb stehen im [Benutzerhandbuch](./docs/use_guide_de.md#operate-an-instance).

<a id="resource-management"></a>
### Ressourcenverwaltung

Ressourcenverwaltung ist das benutzerseitige OpenClaw-Konfigurationszentrum mit den Tabs Ressourcen, Ressourcenpakete und Injektionsprotokolle. Sie ist von der Admin-Funktion Security Protection getrennt.

- Channel-Vorlagen, Formular/JSON-Bearbeitung, Klonen und Lifecycle-Steuerung
- Skill-ZIP-Import, Konfliktbehandlung, Download und Loeschen; Skill Hub verwaltet Katalog, Versionen, Veroeffentlichung und Installation
- Scheduled Tasks in einfacher und erweiterter Ansicht; Agent-Ressourcen sind sichtbar, aber hier noch nicht konfigurierbar
- Ressourcenpakete erstellen, bearbeiten, klonen und bei der Instanzerstellung wiederverwenden
- Schreibgeschuetzte Injektionsprotokolle mit Modus, Ressourcen, Umgebungsvariablen, Status und Zeit

Siehe [Resource Management Guide](./docs/resource-management_de.md) und [Skill Hub Guide](./docs/skill-hub-guide_de.md).

<a id="team-workspaces"></a>
### Team-Kollaboration

Teams verwenden einen Leader-vermittelten Ablauf. Sie entstehen aus acht unveraenderlichen integrierten Vorlagen oder einer benutzereigenen Vorlage. Der OpenClaw Lite Leader plant, zerlegt Aufgaben, verteilt Arbeit, prueft Lieferungen und veroeffentlicht das gemeinsame Ergebnis.

- genau ein OpenClaw Lite Leader; OpenClaw Lite oder Hermes Lite je Worker
- Custom Teams aus natuerlicher Sprache erzeugen, pro Rolle verfeinern, komplett regenerieren und wiederverwenden
- Team Chat fuer Plan, Zuweisung, Fortschritt, Review, Lieferung und Zusammenfassung
- Execution Kanban fuer aktuelle Anfrage, Task Breakdown und Delivery State
- gemeinsame Dateien/Artefakte und native Hermes-Team-Sessions in der Instanzansicht

Siehe [Team Workspace Quick Guide](./docs/team-workspaces-guide_de.md) fuer Erstellung, Kollaborationsphasen und Ergebnisansicht.

<a id="security-protection-platform"></a>
### Security Protection Platform

Security Protection ist ein eigener Admin-Arbeitsbereich mit vier Live-Kennzahlen, Security Events, Pod Live Aegis Configuration, Report-Export und Emergency Circuit Breaker. Die Uebersicht bezeichnet das KSecure-Modell derzeit als sieben Risikoflaechen, fuenfzehn Szenarien und vier Schichten und fuehrt zu Runtime-Abwehr, Host/Container-Isolation, Component Trust, Identity/Outbound, Policies, Collaboration, Quotas, Approval, Skill Scanner und Full-Chain Audit.

Siehe [Security Platform Guide](./docs/security-platform_de.md).

## Produktgalerie

ClawManager ist so gestaltet, dass Administration, Zugriff und AI-Governance nicht wie getrennte Werkzeuge wirken, sondern wie eine zusammenhaengende Produkterfahrung.

### Lite-Mode-Deployment

Lite Mode stellt OpenClaw-, Hermes-, OpenCode- und DeepSeek-Harness-Instanzen ueber gemeinsame Gateway-Runtime-Pools bereit. Jeder Workspace laeuft als isolierter Gateway-Prozess in verwalteten Runtime-Pods. Das sorgt fuer schnelle Starts und reduziert dedizierte CPU-, Memory-, Storage- und GPU-Allocation, waehrend Workspace-Zugriff, Share Link / Password Access, unterstuetzte channel- und skill-Injection sowie Admin-Sichtbarkeit erhalten bleiben.

![](./docs/main/liteopenclaw.png)

### Pro-Mode-Deployment

Pro Mode stellt fuer jede Instanz eine dedizierte Desktop-Runtime bereit, gestuetzt durch ein eigenes Kubernetes Deployment, einen Service und eine PVC. Es eignet sich fuer Nutzer, die staerkere Isolation, volle Desktop-Ressourcen, Runtime Events, Instanz-Skill-Verwaltung und die vollstaendige Desktop-Management-Erfahrung benoetigen.

![](./docs/main/proopenclaw.png)

### Team Workspace

Der Team Workspace zeigt Nachrichten und Lieferungen links sowie die aktuelle Anfrage, Aufgabenteilung, Status und Artefaktdetails im Execution Kanban rechts.

<p align="center">
  <img src="./docs/main/team-collaboration.png" alt="ClawManager Team Workspace und Execution Kanban" width="100%" />
</p>

### Ressourcenverwaltung

Channels, Skills, Scheduled Tasks, Ressourcenpakete und Injektionsprotokolle werden in einem Benutzerzentrum verwaltet; Security Protection bleibt eine separate Admin-Funktion.

<p align="center">
  <img src="./docs/main/resource-management-current.png" alt="ClawManager Ressourcenverwaltung" width="100%" />
</p>

### Security Protection

Die separate Security-Konsole zeigt Live-Kennzahlen und Ereignisse, das KSecure-Schichtenmodell, Pod-Aegis-Konfiguration, Report-Export und Emergency Circuit Breaker.

<p align="center">
  <img src="./docs/main/security-protection-current.png" alt="ClawManager Security Protection" width="100%" />
</p>

### Admin Console

Die Admin-Konsole vereint Nutzer, Quotas, Runtime-Operationen, Security-Kontrollen und plattformweite Richtlinien in einer Oberflaeche. Sie ist die zentrale Arbeitsflaeche fuer Teams, die AI-Agent-Infrastruktur im grossen Massstab betreiben.

<p align="center">
  <img src="./docs/main/admin-current.png" alt="ClawManager Admin Console" width="100%" />
</p>

### Portal Access

Das Portal bietet Nutzern einen klaren Einstiegspunkt in ihre Workspaces. Der Zugriff erfolgt browserbasiert, waehrend Runtime-Zustand und Plattformsicht erhalten bleiben, ohne dass Infrastrukturdetails direkt exponiert werden.

<p align="center">
  <img src="./docs/main/portal-current.png" alt="ClawManager Portal Access" width="100%" />
</p>

### AI Gateway

AI Gateway integriert Modell-Governance direkt in die Workspace-Erfahrung. Audit-Trails, Kostentransparenz und risikobasiertes Routing machen AI-Nutzung zu einem Teil der Plattform statt zu einer losen Einzelintegration.

<p align="center">
  <img src="./docs/main/ai-gateway-current.png" alt="ClawManager AI Gateway" width="100%" />
</p>

## So funktioniert es

1. Administratoren definieren Governance-Richtlinien und wiederverwendbare Ressourcen.
2. Nutzer erstellen oder betreten verwaltete AI-Agent-Workspaces auf Kubernetes.
3. Team Workspaces koennen mehrere Member-Runtimes mit Redis Team Bus und Shared-Storage-Konfiguration bereitstellen.
4. Agents verbinden sich mit der Control Plane und melden Runtime-Zustaende.
5. Channel, skill und bundle werden kompiliert und auf Instanzen angewendet.
6. AI-Traffic fliesst ueber das AI Gateway und erhaelt Audit-, Risiko- und Kostenkontrollen.

## Entwicklerueberblick

ClawManager ist eine Kubernetes-native Plattform mit React-Frontend, Go-Backend, MySQL fuer Zustandsdaten sowie Integrationen wie `skill-scanner` und Object Storage. Die Codebasis ist nach Produktsubsystemen organisiert, daher ist der schnellste Einstieg, mit dem passenden Guide zu beginnen und danach in den Code zu gehen.

- Frontend fuer Admin- und Nutzeroberflaechen unter `frontend/`
- Backend-Services, Handler, Repositorys und Migrationen unter `backend/`
- Deployment-Assets unter `deployments/`
- Produktdokumentation und Medien unter `docs/`

Technische Runtime- und Protokollreferenzen bleiben fuer Mitwirkende unter `docs/`; die folgende Benutzerdokumentation ist nach Produkt-Workflows geordnet.

## Dokumentation

- [Benutzerhandbuch](./docs/use_guide_de.md)
- [Team Workspace Quick Guide](./docs/team-workspaces-guide_de.md)
- [Deployment Guide](./docs/deployment_de.md)
- [AI Gateway Guide](./docs/aigateway_de.md)
- [Security Platform Guide](./docs/security-platform_de.md)
- [Resource Management Guide](./docs/resource-management_de.md)
- [Skill Hub Guide](./docs/skill-hub-guide_de.md)
- [OpenCode Workspace Guide](./docs/opencode-lite-pro-agent-development_de.md)

## Lizenz

Dieses Projekt steht unter der MIT License.

## Open Source

Issues und Pull Requests sind willkommen.

## Star History

<a href="https://github.com/Yuan-lab-LLM/ClawManager/actions/workflows/update-star-history.yml">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-dark.svg" />
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
   <img alt="Star History Chart" src="https://raw.githubusercontent.com/Yuan-lab-LLM/ClawManager/star-history/star-history-light.svg" />
 </picture>
</a>
