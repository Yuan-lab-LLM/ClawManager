[← Zurück zur README](../README.de.md)

# Kurzanleitung für Team-Workspaces

Ein Team verwendet einen OpenClaw-Lite-Leader, der mehrere Worker für ein gemeinsames Ziel koordiniert. Du kannst eine unveränderliche integrierte Vorlage verwenden oder aus einer natürlichsprachlichen Beschreibung eine eigene Vorlage erzeugen. Der Leader versteht das Ziel, verteilt Aufgaben, sammelt Ergebnisse, behandelt Ausnahmen und veröffentlicht das Endergebnis.

## Geltungsbereich

- Die Zusammenarbeit ist fest **Leader-vermittelt**: Anfragen erreichen zuerst den Leader, der anschließend die Mitglieder koordiniert.
- Der Leader verwendet immer **OpenClaw Lite**. Jeder Worker kann **OpenClaw Lite** oder **Hermes Lite** verwenden.
- Wenn kein aktiviertes Hermes-Lite-Gateway-Image konfiguriert ist, ist Hermes Lite deaktiviert und die Ursache wird angezeigt.
- Integrierte Vorlagen können nicht geändert oder gelöscht werden. Eigene Vorlagen gehören dem aktuellen Benutzer und können angepasst, gelöscht und wiederverwendet werden.

## 1. Team aus einer integrierten Vorlage erstellen

1. Öffne **Teams** in der Navigation und rufe die Erstellungsseite auf.
2. Gib einen Teamnamen ein und passe bei Bedarf den gemeinsamen Speicher an.
3. Wähle eine Vorlage und für jeden Worker eine verfügbare Runtime.
4. Prüfe die Zusammenfassung und wähle **Erstellen**.

Die Aktion **+ Benutzerdefiniertes Team** oben rechts öffnet die Verwaltung eigener Vorlagen. Die Runtime der Worker wird in der Mitgliedertabelle derselben Seite ausgewählt.

![Integrierte Vorlagen, Einstieg in benutzerdefinierte Teams und Runtime-Auswahl](./main/team-create-fixed-and-custom-entry.png)

Es gibt acht unveränderliche Vorlagen: Standard mit zwei Mitgliedern, Delivery mit drei Mitgliedern, Product Discovery mit vier Mitgliedern, Quality Gate mit vier Mitgliedern, Full-stack Delivery mit fünf Mitgliedern, API Integration mit fünf Mitgliedern, Research Publication mit sechs Mitgliedern und Software Engineering mit acht Mitgliedern. Rollen und Verantwortlichkeiten sind bereits enthalten; einzelne Ressourcenprofile müssen nicht eingerichtet werden.

## 2. Benutzerdefiniertes Team erzeugen

Öffne **Benutzerdefiniertes Team** und beschreibe das gewünschte Ziel. Lasse die Mitgliederzahl für eine automatische Auswahl leer oder wähle insgesamt 2–6 Mitglieder.

![Benutzerdefiniertes Team aus natürlicher Sprache und Mitgliederzahl erzeugen](./main/custom-team-generate.png)

Erzeugung und Rollenänderungen verwenden das AI Gateway des aktuellen Benutzers mit `model: "auto"`. Das Gateway wählt das tatsächliche Modell; dessen gespeicherte Thinking-Einstellung gilt. Auf der Team-Seite gibt es keinen eigenen Thinking-Schalter. Ist kein Modell verfügbar, fordert die Seite dazu auf, zuerst ein Modell in der Modellverwaltung zu aktivieren.

Jedes Ergebnis erfüllt folgende Regeln:

- Das Team umfasst 2–6 Mitglieder.
- Das erste und einzige Leader-Mitglied behält `memberId=leader`.
- Fähigkeits-Tags beschreiben geeignete Fähigkeiten; sie installieren keine Skills und ändern keine Runtime-Konfiguration.

## 3. Eigene Vorlagen verwalten

Unter **Meine benutzerdefinierten Teams** kannst du eine Vorlage auswählen und:

- umbenennen;
- nach Änderung von Ziel oder Mitgliederzahl das gesamte Team aktualisieren;
- das gesamte Team aus dem gespeicherten Ziel und der gespeicherten Anzahl neu erzeugen;
- die Vorlage löschen oder auf der Team-Erstellungsseite verwenden.

![Vorhandene benutzerdefinierte Team-Vorlagen verwalten](./main/custom-team-manage.png)

Jede Aktualisierung erzeugt eine neue Version. Integrierte Vorlagen erscheinen nicht in der bearbeitbaren Liste.

## 4. Verantwortlichkeiten anpassen

Öffne ein Mitglied und beschreibe die gewünschte Änderung in natürlicher Sprache. Leere Eingaben werden nicht gesendet und führen zu einem klaren Hinweis.

![Verantwortlichkeiten eines Mitglieds natürlichsprachlich anpassen](./main/custom-team-member-adjustment.png)

Auch der Leader kann angepasst werden, jedoch nur in seiner fachlichen Erweiterung. Leader-Identität, unveränderliche Orchestrierung, aktuelle Worker-Liste sowie Delegation, Ergebnissammlung, Prüfung und finale Zusammenfassung bleiben erhalten. Nach einer Änderung der Worker-Zahl erhält der Leader über den bestehenden Team-Start weiterhin die vollständige Mitgliederliste und alle Verantwortlichkeiten.

## 5. Zusammenarbeit starten und verfolgen

Beschreibe nach der Erstellung das Ziel im Team-Chat. Der Leader plant, delegiert, sammelt Ergebnisse und Review-Nachweise und veröffentlicht die finale Zusammenfassung. Der Abschluss eines Workers beendet nur dessen Arbeitselement; die Gesamtaufgabe endet nach der Zusammenfassung des Leaders.

Die Team-Detailseite enthält:

- **Team-Chat** für Pläne, Zuweisungen, relevante Fortschritte, Ergebnisse, Reviews und finale Zusammenfassung.
- **Execution Kanban** mit der aktuellen Anfrage im Kopfbereich sowie Gesamt- und Mitgliederstatus.
- **Anfragenavigation** ab zwei Anfragen; eine neue Anfrage wird automatisch ausgewählt.
- **Files** für gemeinsame Artefakte. Markdown, Text und JSON können direkt angesehen, andere Dateien heruntergeladen werden.

Monitor beobachtet Aktivität, Abschlussbelege und Fehlersignale für Erinnerungen und Wiederherstellung. Er erzeugt nicht selbstständig Erfolg, Fehler, Abbruch oder Abschluss eines Tasks.

## 6. Hermes-Lite-Worker-Sitzungen

Hermes-Lite-Team-Unterhaltungen verwenden den nativen Hermes-Sitzungsspeicher. Vollständige Nachrichten und Tool-Ergebnisse erscheinen während der Ausführung schrittweise in der Hermes-GUI und nicht erst nach Abschluss als Historie.

Wird dieselbe Hermes-Lite-Instanz über ein Team-Mitglied oder die Instanzliste geöffnet, ist dieselbe Team-Sitzung sichtbar und kann fortgesetzt werden. Normale Hermes-Sitzungen bleiben unverändert. Sitzungen dienen Interaktion und Beobachtung; für Kanban und Abschluss bleibt der Team-Kontrollpfad maßgeblich.

## 7. Empfehlungen

- Beginne mit der ähnlichsten integrierten Vorlage und erzeuge eine eigene Vorlage für dauerhaft wiederverwendbare Spezialrollen.
- Nenne Umfang, Datenquellen, Ausgabeformat und Abnahmekriterien im Ziel.
- Sende dieselbe Anfrage nicht erneut, nur weil ein Worker geliefert hat; warte auf Prüfung und Zusammenfassung des Leaders.
- Thinking kann Latenz und Reasoning-Token erhöhen. Konfiguriere es passend zur Aufgabe in der Modellverwaltung.
