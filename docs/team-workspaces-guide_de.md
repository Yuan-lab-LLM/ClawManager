[← Zurück zur README](../README.de.md)

# Kurzanleitung für Team-Workspaces

Ein Team verwendet einen OpenClaw-Lite-Leader, der mehrere Worker für ein gemeinsames Ziel koordiniert. Du kannst eine unveränderliche integrierte Vorlage verwenden oder aus einer natürlichsprachlichen Beschreibung eine eigene Vorlage erzeugen. Der Leader versteht das Ziel, verteilt Aufgaben, sammelt Ergebnisse, behandelt Ausnahmen und veröffentlicht das Endergebnis.

Die vorhandenen Screenshots zeigen eine frühere Oberfläche. Das aktuelle fortlaufende Layout, die Avatar-Auswahl, Einzelaufgaben und Dateibearbeitung werden im folgenden Text beschrieben.

## Geltungsbereich

- Standardmäßig verwendet `@Leader` die **Leader-vermittelte Zusammenarbeit**. Ein per `@` ausgewählter Worker kann eine neue Aufgabe auch allein ausführen.
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

Funktions-Avatare werden Rollen automatisch zugeordnet; ohne eindeutige Zuordnung erscheint ein allgemeines Symbol. Klicke bei der Team-Erstellung oder Vorlagenbearbeitung auf den Worker-Avatar, um ein anderes Symbol zu wählen. Das ändert nur die Darstellung, nicht Verantwortlichkeiten, Tool-Berechtigungen oder Runtime-Einstellungen. Leader und Benutzer behalten eigene Kennzeichnungen.

## 4. Verantwortlichkeiten anpassen

Öffne ein Mitglied und beschreibe die gewünschte Änderung in natürlicher Sprache. Leere Eingaben werden nicht gesendet und führen zu einem klaren Hinweis.

![Verantwortlichkeiten eines Mitglieds natürlichsprachlich anpassen](./main/custom-team-member-adjustment.png)

Auch der Leader kann angepasst werden, jedoch nur in seiner fachlichen Erweiterung. Leader-Identität, unveränderliche Orchestrierung, aktuelle Worker-Liste sowie Delegation, Ergebnissammlung, Prüfung und finale Zusammenfassung bleiben erhalten. Nach einer Änderung der Worker-Zahl erhält der Leader über den bestehenden Team-Start weiterhin die vollständige Mitgliederliste und alle Verantwortlichkeiten.

## 5. Zusammenarbeit starten und verfolgen

Beschreibe nach der Erstellung das Ziel für den Standardempfänger `@Leader`. Der Leader plant, delegiert, sammelt Ergebnisse und Review-Nachweise und veröffentlicht die finale Zusammenfassung. In diesem Modus beendet ein Worker-Abschluss nur dessen Arbeitselement; die Gesamtaufgabe endet nach der Leader-Zusammenfassung. Einzelaufgaben werden unten beschrieben.

Die Team-Detailseite enthält:

- **Team-Chat** mit hellem, fortlaufendem Layout für Pläne, Zuweisungen, Fortschritte, Ergebnisse, Reviews und Zusammenfassung. Nachrichten respektieren Mitglieder- und Aufgabengrenzen; Benutzernachrichten bleiben getrennt.
- **Execution Kanban** mit der aktuellen Anfrage im Kopfbereich sowie Gesamt- und Mitgliederstatus.
- **Anfragenavigation** ab zwei Anfragen; eine neue Anfrage wird automatisch ausgewählt.
- **Mitgliederleiste** mit Funktions-Avataren, Status und Instanzzugang. Ablauf- und Dateibereiche sind aufklappbar; schmale Ansichten verwenden seitliche Schubladen.
- **Files** zum Durchsuchen, Ansehen, Herunterladen und Online-Bearbeiten unterstützter gemeinsamer Textdateien mit entsprechender Berechtigung.

Monitor beobachtet Aktivität, Abschlussbelege und Fehlersignale für Erinnerungen und Wiederherstellung. Er erzeugt nicht selbstständig Erfolg, Fehler, Abbruch oder Abschluss eines Tasks.

### 5.1 Aufgaben per `@Mitglied` und Überarbeitung früherer Ergebnisse

1. Tippe `@` oder öffne die Empfängerauswahl über dem Eingabefeld, suche einen Worker und wähle ihn aus.
2. Sende die Frage oder Änderung. Mit dem Leader oder dem Standardempfänger bleibt die normale Team-Zusammenarbeit erhalten.
3. Jede Einzelanfrage erzeugt eine eigene Aufgabe mit eigenem Verlauf und Kanban-Eintrag. Sie endet nach Annahme der formalen Worker-Lieferung, ohne Leader-Zusammenfassung.
4. Wähle für ältere Artefakte zuerst eine Referenzaufgabe im Verlauf und dann den zugehörigen Worker. Ohne ausdrückliche Auswahl wird dessen jüngste tatsächliche Arbeit bevorzugt. Unnötige Referenzen lassen sich im Eingabebereich entfernen.

Eine Referenz liefert Kontext für die neue Aufgabe; sie öffnet die alte Aufgabe nicht erneut und löscht keine bestehende Mitgliedersitzung. Es gibt einen Ausführenden, keinen Rundruf an alle im Text genannten Namen. Enter sendet, Shift+Enter fügt eine neue Zeile ein.

### 5.2 Gemeinsame Dateien online bearbeiten

Öffne eine unterstützte Textdatei im Editor des Dateibrowsers. Bearbeitbar sind berechtigte **UTF-8-Dateien bis 1 MiB**, etwa Markdown, JSON, YAML sowie übliche Quell- und Konfigurationsdateien. Für andere Dateien bleiben die verfügbaren Vorschau- und Download-Aktionen erhalten.

- **Speichern** oder Ctrl+S (Cmd+S unter macOS) schreibt die Änderungen explizit. JSON und YAML werden vorher auf Syntaxfehler geprüft.
- Externe Änderungen werden regelmäßig erkannt, der lokale Entwurf bleibt erhalten. Kopiere oder lade ihn bei einem Versionskonflikt herunter, bevor du neu lädst und zusammenführst; externe Inhalte überschreiben den Entwurf nicht automatisch.
- Das Schließen ohne Speichern erfordert eine Bestätigung. Speichern startet weder die Instanz neu noch Skripte oder ein Neuladen der Runtime-Konfiguration.
- Kein automatisches Speichern und kein gleichzeitiges Echtzeit-Editieren. Worker nehmen nicht an der Editorsperre teil: Warte bei noch erzeugten Dateien auf die Lieferung, bevor du sie bearbeitest.

Neue Funktionen benötigen passende ClawManager-App- und Runtime-Versionen. Ein Runtime-Update allein aktualisiert die Team-Seite nicht und belegt keine erfolgreiche Modell-, Kanal- oder Aufgabenabnahme.

## 6. Hermes-Lite-Worker-Sitzungen

Hermes-Lite-Team-Unterhaltungen verwenden den nativen Hermes-Sitzungsspeicher. Vollständige Nachrichten und Tool-Ergebnisse erscheinen während der Ausführung schrittweise in der Hermes-GUI und nicht erst nach Abschluss als Historie.

Wird dieselbe Hermes-Lite-Instanz über ein Team-Mitglied oder die Instanzliste geöffnet, ist dieselbe Team-Sitzung sichtbar und kann fortgesetzt werden. Normale Hermes-Sitzungen bleiben unverändert. Sitzungen dienen Interaktion und Beobachtung; für Kanban und Abschluss bleibt der Team-Kontrollpfad maßgeblich.

## 7. Empfehlungen

- Beginne mit der ähnlichsten integrierten Vorlage und erzeuge eine eigene Vorlage für dauerhaft wiederverwendbare Spezialrollen.
- Nenne Umfang, Datenquellen, Ausgabeformat und Abnahmekriterien im Ziel.
- Sende im gemeinsamen Modus dieselbe Anfrage nicht erneut, nur weil ein Worker geliefert hat; warte auf Prüfung und Leader-Zusammenfassung. Für einzelne Änderungen erstelle eine neue `@Worker`-Aufgabe mit Bezug auf die frühere Arbeit.
- Thinking kann Latenz und Reasoning-Token erhöhen. Konfiguriere es passend zur Aufgabe in der Modellverwaltung.
