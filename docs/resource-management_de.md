[← Zurück zur README](../README.de.md)

# Leitfaden zur Ressourcenverwaltung

Die Ressourcenverwaltung ist das Benutzerzentrum fuer wiederverwendbare OpenClaw-Startkonfigurationen. Sie ist von **Security Protection** getrennt: Hier werden Konfigurationen vorbereitet und ausgeliefert; Security Protection ist eine eigenstaendige Admin-Funktion fuer Risiko und Governance.

![OpenClaw Ressourcenverwaltung](./main/resource-management-current.png)

## Drei Bereiche

- **Ressourcen**: einzelne Definitionen suchen und verwalten.
- **Ressourcenpakete**: mehrere Ressourcen zu einer wiederverwendbaren Startauswahl kombinieren.
- **Injektionsprotokolle**: die beim Erstellen einer Instanz kompilierten und beim Neustart wiederverwendeten Snapshots pruefen.

## Ressourcentypen

- **Channels**: Kommunikationskonfigurationen erstellen, bearbeiten, aktivieren/deaktivieren, klonen und loeschen. Unterstuetzte Vorlagen wie Telegram, DingTalk, WeCom, Slack und Feishu bieten Formular- und JSON-Bearbeitung.
- **Skills**: ein oder mehrere ZIP-Pakete hochladen, Importkonflikte aufloesen, Pakete herunterladen oder loeschen. Katalog, Eigentum, Versionen, Veroeffentlichung und spaetere Installation gehoeren zum **Skill Hub**.
- **Agents**: als reservierter Typ sichtbar, derzeit auf dieser Seite nicht konfigurierbar.
- **Scheduled Tasks**: wiederverwendbare OpenClaw-Jobs per einfachem Formular oder erweitertem JSON verwalten; cron, Intervall und einmalige Ausfuehrung sowie Announce, Webhook oder keine Zustellung werden unterstuetzt.

Session Templates und Log Policies existieren im Ressourcenmodell, sind in dieser UI aber absichtlich ausgeblendet.

## Ressourcenpakete und Injektionsprotokolle

Ressourcenpakete koennen erstellt, bearbeitet, aktiviert/deaktiviert, geklont und geloescht werden. Sie kombinieren aktivierte Ressourcen und geeignete Skills fuer wiederholbare Instanz-Setups.

Injektionsprotokolle sind schreibgeschuetzte Liefernachweise mit Snapshot-ID, Modus, Ressourcenanzahl, Anzahl der Umgebungsvariablen, Status und Erstellungszeit. Sie zeigen, was kompiliert wurde; sie sind keine Sicherheitsereignisse.

## Abgrenzung

- **Skill Hub** verwaltet Katalog, Versionen, Veroeffentlichung und Installation fuer OpenClaw, Hermes, OpenCode und DeepSeek Harness.
- **Instanzerstellung** bietet je nach Runtime Archive, Pakete, einzelne Ressourcen oder Skills an.
- **Security Protection** ist die separate Admin-Konsole fuer Runtime-Abwehr, Isolation, Richtlinien, Notfallsteuerung und Audit. Skill Scanner ist dort ein Szenario, kein Tab der Ressourcenverwaltung.

Siehe auch [Skill Hub](./skill-hub-guide_de.md), [Security Protection](./security-platform_de.md) und [Benutzerhandbuch](./use_guide_de.md).
