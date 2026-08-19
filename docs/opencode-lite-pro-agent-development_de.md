[← Zurueck zur README](../README.de.md)

# OpenCode Workspace Guide

OpenCode ist der verwaltete Coding-Workspace von ClawManager. Er nutzt die offizielle OpenCode-Version und greift ueber AI Gateway auf Modelle zu.

## Lite und Pro

| Modus | Form | Grenze |
|---|---|---|
| Lite | isolierter Prozess/Workspace im gemeinsamen Runtime Pod | kein eigener Pod pro Instanz |
| Pro | dedizierter Desktop-Workload | gespeicherte Default-Images ersetzen bestehende Instanzen nicht automatisch |

Beide Modi persistieren den Workspace nach dem gewaehlten Storage-Profil. ClawManager passt das Lite-Portal an; Pro oeffnet OpenCode im dedizierten Desktop.

## Vor Erstellung

Der Administrator aktiviert ein kompatibles OpenCode-Image und mindestens ein normales AI-Gateway-Modell. Fuer Lite muss der gemeinsame Pool gesund sein; nach dem Speichern eines neuen Images ist zusaetzlich der Lite Rolling Upgrade erforderlich. Benutzer brauchen ausreichende Ressourcenquote.

OpenCode erhaelt eine verwaltete AI-Gateway-Providerkonfiguration. Keine fremden Provider-Keys ueber OpenCode hinzufuegen, sofern der Administrator dies nicht ausdruecklich vorgesehen hat.

## Nutzung

Unter **My Instances → Create** OpenCode und Lite/Pro waehlen, danach Image, Ressourcen, Environment und angebotene Startressourcen. Die Instanzseite bietet Lifecycle, Terminal/Desktop und Files.

- Start/Stop/Restart/Delete ueber ClawManager ausfuehren.
- Projekte im angezeigten Workspace statt in temporaeren Verzeichnissen speichern.
- File Panel fuer Upload/Download/Edit/Delete verwenden, soweit Storage es unterstuetzt.
- Stream-Profil und Environment-Aenderungen brauchen meist Apply/Restart.
- Share Link mit Ablauf, Credential und minimaler Workspace-Berechtigung erstellen.

## AI Gateway und Skills

Bei Modellfehlern erst Instanzstatus, dann Model Health, Protokoll und AI Audit pruefen. Ein Security Model ist fuer normale Nutzung nicht erforderlich.

Skill Hub ist eine gemeinsame Plattformfunktion fuer OpenClaw, Hermes, OpenCode und DeepSeek Harness. OpenCode Lite materialisiert nach `{workspace}/home/.opencode/skills`, managed HostPath Pro nach `/config/workspace/.opencode/skills`. Fehlt die Auswahl bei Creation, danach installieren und in Skill Management verifizieren. Non-HostPath Pro benoetigt passende Runtime-Agent-Commands.

## Grenzen und Fehlerbehebung

- Keine OpenClaw-Config-Plans, OpenClaw-Archive oder Team-Persona-Injection.
- Standard-Team-Erstellung nutzt OpenCode derzeit nicht als Leader/Worker.
- Scheduled Tasks gelten nur als verfuegbar, wenn die UI sie zeigt.
- Altes Lite-Image: Rolling Upgrade nach Save ausfuehren.
- Portal nicht erreichbar: Instanz/Pool Health und Events pruefen.
- Files verschwinden: Workspace-Pfad, PVC und Storage-Profil pruefen.
- Skill unvollstaendig: Materialisierung und Runtime-Agent-Faehigkeit pruefen.

Abnahme: Create/Start/Stop/Restart, Portal/Desktop, Streaming/Tools, Persistenz, Share Link und verwendete Skill-Flows. Siehe [Benutzerhandbuch](./use_guide_de.md), [AI Gateway](./aigateway_de.md) und [Skill Hub](./skill-hub-guide_de.md).
