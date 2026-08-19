[← Zurueck zur README](../README.de.md)

# Skill Hub Benutzerhandbuch

Skill Hub ist der gemeinsame, versionierte Skill-Katalog fuer OpenClaw, Hermes, OpenCode und DeepSeek Harness. Er macht aus Dateien einer Instanz gepruefte, veroeffentlichbare und erneut installierbare Assets; er ist keine OpenCode-Sonderfunktion.

## Ansichten

- **Browse**: veroeffentlichte Skills suchen, Tags filtern, Autor, Version, Scan und Risiko pruefen und kompatibel installieren.
- **My Skills**: eigene Uploads oder aus Instanzen gesammelte Skills, Versionen, Tags, Publish/Unpublish, Download und Delete verwalten.
- **Admin**: Plattform-Skills und erlaubte Governance-Aktionen anzeigen; Buttons bleiben von Ownership und Versionsstatus abhaengig.
- **Instance Skill Management**: installierte, Hub-verwaltete und im Workspace entdeckte Skills einer Instanz pruefen.

## Upload, Scan und Publish

1. Unter My Skills ein oder mehrere ZIPs mit `SKILL.md` und benoetigten Dateien hochladen.
2. ClawManager speichert eine Version und startet den Security Scan; gaengige Windows/CJK-ZIP-Namen werden kompatibel behandelt.
3. Scanstatus, Risiko und Findings lesen. Failed bleibt sichtbar, damit eine korrigierte Version hochgeladen werden kann.
4. Wenn Version und Policy passen, Tags setzen und veroeffentlichen.
5. Unpublish entfernt die Version aus Browse, loescht aber nicht die Historie des Owners.

Scan completed bedeutet nicht automatisch risikofrei oder freigegeben. Package, Ownership, Runtime-Kompatibilitaet und Plattform-Policy bleiben massgeblich.

## Installation und Verifikation

Skill oeffnen, Install waehlen, eine oder mehrere kompatible Instanzen und Version bestaetigen. Danach in jeder Instanz Skill Management aktualisieren und die effektive Version pruefen. OpenClaw, Hermes, OpenCode und DeepSeek Harness werden unterstuetzt; Materialisierung und Reload sind Runtime-spezifisch. DeepSeek Harness nutzt `home/.dsh/skills` in Lite und `.dsh/skills` in Pro.

## Aus einer Instanz sammeln

Workspace Discovery legt nichts automatisch in My Skills ab. Dateien und Quelle pruefen, **Collect to library** ausfuehren, Packaging/Scan abwarten und erst danach veroeffentlichen. Bei Drift entweder installierte Version wiederherstellen oder den aktuellen Stand als neue Version sammeln; Historie nicht still ueberschreiben.

## Grenzen und Fehlerbehebung

- Vorhandenes YAML Frontmatter in `SKILL.md` wird nicht umgeschrieben; `name` und `description` vorher pruefen.
- Capability Tags dienen Beschreibung und Suche, nicht automatischer Installation.
- Publish deaktiviert: Scan, Paket, Ownership und Risk/Policy pruefen.
- Zielinstanz fehlt: Ownership, Runtime-Support und Instanzstatus pruefen.
- Skill nach Installation unsichtbar: Skill Management aktualisieren, Version/Pfad pruefen und Runtime bei Bedarf neu laden.

Siehe [Resource Management](./resource-management_de.md), [Security Protection](./security-platform_de.md) und [Benutzerhandbuch](./use_guide_de.md).
