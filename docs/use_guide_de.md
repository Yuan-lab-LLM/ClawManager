[← Zurueck zur README](../README.de.md)

# ClawManager Benutzerhandbuch

Dieses zentrale Handbuch beschreibt die aktuelle Bedienung fuer Benutzer und Administratoren. Nur umfangreiche Spezialthemen bleiben in eigenen Guides.

## Inhaltsverzeichnis

- [1. Deployment und Anmeldung](#deploy-and-sign-in)
- [2. Rollen und Navigation](#roles-and-navigation)
- [3. Modelle konfigurieren](#configure-model-access)
- [4. Workspace erstellen](#create-a-workspace)
- [5. Instanz bedienen](#operate-an-instance)
- [6. Ressourcen und Skills](#resources-and-skills)
- [7. Team-Kollaboration](#team-collaboration)
- [8. Administration](#administration)
- [9. Runtime-Images und Lite-Rollout](#runtime-images-and-rollout)
- [10. AI Gateway und Session Usage](#ai-gateway)
- [11. Security Protection](#security-protection)
- [12. Zwischenablage und Desktop](#clipboard-and-desktop)
- [13. Fehlerbehebung und Abnahme](#troubleshooting)
- [14. Spezial-Guides](#focused-guides)

<a id="deploy-and-sign-in"></a>
## 1. Deployment und Anmeldung

Es gibt vier gepflegte Profile: k3s oder Kubernetes, jeweils als Single-Node HostPath oder Multi-Node CSI/RWX. Nur ein vollstaendiges Profil anwenden; keine Manifeste mischen und Multi-Node-Storage nicht mit temporaerem HostPath reparieren. Nach Bereitschaft von Workloads und PVCs die konfigurierte Adresse oeffnen und mit einem vom Administrator angelegten Konto anmelden. Details und ARM64-Pruefungen: [Deployment Guide](./deployment_de.md).

<a id="roles-and-navigation"></a>
## 2. Rollen und Navigation

Der Benutzerbereich enthaelt Workbench, My Instances, Teams, Resource Management, Skill Hub und Settings. Resource Management verwaltet Startressourcen; Skill Hub ist der gemeinsame versionierte Skill-Katalog fuer alle unterstuetzten Runtimes. Der Adminbereich ergaenzt Users, alle Instances, Runtime-Pools, Security Protection, AI Gateway und Systemeinstellungen. Sichtbare Aktionen haengen von Rolle und Quote ab.

<a id="configure-model-access"></a>
## 3. Modelle konfigurieren

Unter **AI Gateway → Models** mindestens ein normales Modell anlegen, aktivieren und testen. Ein Security Model ist nur fuer Risk Rules mit sicherem Routing erforderlich. Managed Thinking ist eine persistente Modelleinstellung fuer kontrollierbare Provider/Modelle; es kann Latenz und Reasoning Tokens erhoehen, zeigt aber keine private Gedankenkette.

<a id="create-a-workspace"></a>
## 4. Workspace erstellen

Unter **My Instances → Create** Runtime und Modus waehlen:

| Runtime | Verwendung | Lite | Pro |
|---|---|---|---|
| OpenClaw | Sessions, Tools, Scheduled Tasks, Team Leader/Worker | gemeinsamer Pool | dedizierter Desktop |
| Hermes | native Hermes-Sessions/Tools, optional Team Worker | gemeinsamer Pool | dedizierter Desktop |
| OpenCode | Coding-Workspace mit AI Gateway, Dateien, Terminal/Desktop | gemeinsamer Pool | dedizierter Desktop |
| DeepSeek Harness | verwalteter Agent-Workspace mit AI Gateway, Skills, Workspace-Dateien und nativer Browser-UI | gemeinsamer Pool | dedizierter Webtop |

Je nach Auswahl erscheinen Image, Ressourcenpreset oder CPU/Memory/Storage, Stream-Profil, Umgebungsvariablen, Archivimport, Resource Pack, einzelne Ressourcen und Skills. Lite erstellt keinen Pod pro Instanz, sondern einen isolierten Workspace/Prozess im gemeinsamen Runtime Pod.

Skill Hub ist keine OpenCode-Sonderfunktion: OpenClaw, Hermes, OpenCode und DeepSeek Harness verwenden denselben Katalog. Nur Zielpfad und Reload unterscheiden sich. Fehlt die Skill-Auswahl bei der Erstellung, nach Bereitstellung ueber Skill Hub oder die Instanzseite installieren.

<a id="operate-an-instance"></a>
## 5. Instanz bedienen

- Start, Stop und Restart ueber ClawManager ausfuehren; Environment Overrides werden ueber den vorgesehenen Restart angewendet.
- Vor Delete benoetigte Dateien/Archive sichern.
- Share Link mit Kennwort, Ablauf und Workspace-Berechtigung konfigurieren und spaeter widerrufen.
- Workspace-Dateien je nach Runtime/Storage anzeigen, hochladen, laden, bearbeiten oder loeschen.
- Low/Standard/High aendern Bandbreite und Bildqualitaet; gespeicherte Stream-Aenderungen brauchen normalerweise Restart/Apply.
- Skill Management zeigt installierte/entdeckte Versionen; Session Usage zeigt nur vom Runtime gemeldete Daten.
- Dedicated Instances koennen Runtime Overview und Events fuer Diagnose zeigen.

<a id="resources-and-skills"></a>
## 6. Ressourcen und Skills

Resource Management besitzt **Resources**, **Resource Packs** und schreibgeschuetzte **Injection Records**. Resources umfassen Channels, hochgeladene Skill-Pakete und Scheduled Tasks; der Typ Agent ist derzeit reserviert.

Skill Hub ist die Runtime-uebergreifende Plattform fuer Browse, My Skills, Ownership, Tags, Versionen, Scanstatus, Publication, Installation und Verifikation in der Instanz. ZIP-Pakete brauchen `SKILL.md`. Fehlgeschlagene Scans bleiben zur Korrektur sichtbar; ein abgeschlossener Scan ist keine automatische Freigabe. Unterstuetzt werden OpenClaw, Hermes, OpenCode und DeepSeek Harness. Siehe [Resource Management](./resource-management_de.md) und [Skill Hub](./skill-hub-guide_de.md).

<a id="team-collaboration"></a>
## 7. Team-Kollaboration

Unter **Teams → Create** eine von acht unveraenderlichen Vorlagen oder eine eigene Vorlage waehlen. Der Leader ist OpenClaw Lite; Worker koennen OpenClaw Lite oder Hermes Lite verwenden. Eigene Teams haben 2–6 Mitglieder und koennen per Intent erzeugt, umbenannt, in Intent/Anzahl ueberarbeitet, komplett neu erzeugt, pro Rolle angepasst und geloescht werden. Leader-Anpassungen erweitern nur die Domaenenrolle und entfernen nicht Delegation, Ergebnissammlung oder finalen Bericht.

Chat, neuestes Execution Kanban, Files, Artifacts, Member Deliveries und Ergebnis sind gemeinsam sichtbar. Neue Fragen wechseln standardmaessig zur neuesten Task-Gruppe. Siehe [Team Guide](./team-workspaces-guide_de.md).

<a id="administration"></a>
## 8. Administration

Users verwaltet Konten, Rollen, Quoten und CSV-Import. Instances bietet globale Suche und Lifecycle-Aktionen. Runtime zeigt gemeinsame Pods, Kapazitaet, Gesundheit und Drain fuer Wartung. Settings verwaltet Images und Lite-Rollouts. Security Protection und AI Gateway sind eigene Adminbereiche, nicht Teil von Resource Management.

<a id="runtime-images-and-rollout"></a>
## 9. Runtime-Images und Lite-Rollout

Unter **Admin Console → Settings** Images verwalten:

![Runtime-Image-Einstellungen und Lite-Rollout](./main/runtime-settings-rollout.png)

1. Image in der Lite-/Pro-Karte eintragen und **Save** klicken. Das speichert das Image fuer spaetere Bereitstellung, ersetzt aber keinen laufenden Lite Pod.
2. Fuer den aktiven Lite-Pool oben **Lite Runtime Rolling Upgrade** nutzen: OpenClaw Lite, Hermes Lite, OpenCode Lite oder DeepSeek Harness Lite auswaehlen, Current/Target Image sowie Batch und Max Unavailable pruefen.
3. **Start Rolling Upgrade** startet kontrolliertes Drain und Replacement.
4. Danach Runtime-Gesundheit und eine Testinstanz pruefen.

Groessere Batches sind schneller, reduzieren aber Kapazitaet. Aktive Lite-Sessions koennen beim Drain unterbrochen werden; konservative Werte und Wartungsfenster verwenden. Ein gespeichertes Pro-Image ersetzt bestehende Pro-Instanzen nicht automatisch.

<a id="ai-gateway"></a>
## 10. AI Gateway und Session Usage

Die fuenf Bereiche sind Models, AI Audit, Costs, Session Usage und Risk Rules. Session Usage ist Beobachtung, kein Conversation Editor oder Rechnungsbuch: nach Zeitraum, Benutzer, Runtime, Instanz oder Session filtern und gemeldete Input/Output/Cached/Reasoning Tokens vergleichen; Request-Details in AI Audit untersuchen. Siehe [AI Gateway Guide](./aigateway_de.md).

<a id="security-protection"></a>
## 11. Security Protection

Security Protection ist ein eigener Adminbereich mit Alarmmetriken, Ereignissen, Pod Live Aegis, Export, Notfallsteuerung und KSecure-Modell. Detailseiten decken Runtime-Schutz, Isolation, Trust, Identity/Egress, Policy, Collaboration, Quoten/Approval, Skill Scanner und Audit ab. Benutzer sehen Skill-Scans in Skill Hub; Administratoren verwalten Scanner-Gesundheit und Evidenz hier. Siehe [Security Guide](./security-platform_de.md).

<a id="clipboard-and-desktop"></a>
## 12. Zwischenablage und Desktop

Je nach Runtime-Image ist die Zwischenablage bidirektional, nur Host→Desktop oder deaktiviert. Aenderungen brauchen meist einen Restart. Erst ASCII, dann Unicode/CJK testen; Clipboard und Keyboard/IME sind getrennte Wege. Keine Kennwoerter oder API-Keys zum Testen verwenden.

<a id="troubleshooting"></a>
## 13. Fehlerbehebung und Abnahme

| Problem | Pruefen |
|---|---|
| Runtime/Image fehlt | Image gespeichert und aktiviert? |
| Gespeichertes Lite-Image laeuft nicht | Zusaetzlich Rolling Upgrade starten. |
| Kein Modell | Mindestens ein normales Modell aktivieren. |
| Lite hat keinen eigenen Pod | Erwartet: gemeinsamer Runtime Pod. |
| PVC Pending | Profil, StorageClass, AccessMode, Node-Label, Kapazitaet. |
| Skill fehlt nach Installation | Version/Pfad pruefen, Skill Management aktualisieren, Runtime ggf. neu laden. |
| Session Usage leer | Zeitraum/Filter und Runtime-Metadaten pruefen. |

Abnahme: gesunde Workloads/PVCs, funktionierendes Modell, Testinstanz je freigegebenem Runtime, Lifecycle/Files, Skill-Installation, AI Audit/Session Usage und bei Teams Chat/Kanban/Files/Ergebnis.

<a id="focused-guides"></a>
## 14. Spezial-Guides

- [Deployment](./deployment_de.md)
- [Team](./team-workspaces-guide_de.md)
- [AI Gateway](./aigateway_de.md)
- [Security Protection](./security-platform_de.md)
- [Resource Management](./resource-management_de.md)
- [Skill Hub](./skill-hub-guide_de.md)
- [OpenCode Workspace](./opencode-lite-pro-agent-development_de.md)
