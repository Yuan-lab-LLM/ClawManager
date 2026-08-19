[← Zurueck zur README](../README.de.md)

# AI Gateway Benutzerhandbuch

AI Gateway ist der verwaltete Modellzugang fuer OpenClaw, Hermes, OpenCode, DeepSeek Harness, Teams und Plattformfunktionen.

## Vor der Instanzerstellung

Unter **AI Gateway → Models** mindestens ein normales Modell anlegen, aktivieren und testen. Ein Security Model ist optional und wird nur benoetigt, wenn eine Risk Rule sensible Requests dorthin routet. Fehlt jedes aktive Modell, koennen modellbasierte Instanzen und Custom Teams nicht sinnvoll erstellt werden.

## Fuenf Bereiche

- **Models**: Provider URL, Modell, Credential, Protokoll, Preis, Health, Enable, Security Role und Thinking.
- **AI Audit**: Trace, Routing, Provider Response, Policy Hit, Latenz und Fehler.
- **Costs**: Token-basierte Schaetzung und interne Abrechnungsansicht.
- **Session Usage**: Nutzung nach Benutzer, Runtime, Instanz und Session.
- **Risk Rules**: geordnete Allow-, Block- oder Secure-Route-Entscheidungen.

Managed Thinking ist eine persistente Modelleinstellung fuer Kombinationen, die ClawManager verlaesslich steuern kann. Es kann Latenz und Reasoning Tokens erhoehen und zeigt keine private Gedankenkette. Ist es am Modell deaktiviert, kann ein Runtime es nicht unbemerkt wieder aktivieren.

## Protokolle und Routing

Unterstuetzt werden OpenAI Chat Completions, OpenAI Responses und Anthropic Messages. Das Protokoll muss zum Upstream Provider passen; Streaming und Tool Calls vor Produktion testen. Risk Rules werden in Reihenfolge ausgewertet: blockieren, an ein aktives Security Model routen oder beim normalen Modell bleiben.

## Session Usage

Nach Zeitraum, Benutzer, Runtime, Instanz oder Session filtern. Input, Output, Cached und Reasoning Tokens werden nur angezeigt, wenn Runtime/Provider sie melden; die Kostenschaetzung nutzt den konfigurierten Modellpreis. Fuer Request-Routing, Fehler und Policy-Evidenz **AI Audit** verwenden.

Session Usage ist Beobachtung, kein Conversation Editor und keine Provider-Rechnung. Alte, unterbrochene oder nicht unterstuetzte Sessions koennen unvollstaendig sein. Provider-Summen koennen wegen Retries und unterschiedlicher Token-Kategorien abweichen.

## Fehlerbehebung

| Problem | Pruefen |
|---|---|
| Kein Modell bei Creation | Mindestens ein normales Modell aktiv und gesund. |
| Thinking nicht verfuegbar | Provider/Modell nicht verwaltet; nicht erzwingen. |
| Session Usage leer | Zeitraum, Filter und Runtime-Reporting. |
| Kosten fehlen | Input/Output-Preis des Modells. |
| Request blockiert/umgeleitet | Matching Risk Rule und Reihenfolge in AI Audit. |

Siehe [Benutzerhandbuch](./use_guide_de.md) und [Security Protection](./security-platform_de.md).
