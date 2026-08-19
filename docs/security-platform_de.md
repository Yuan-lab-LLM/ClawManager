[← Zurück zur README](../README.de.md)

# Leitfaden zur Security Protection Platform

**Security Protection** ist ein eigenstaendiger Admin-Arbeitsbereich und kein Teil der benutzerseitigen Ressourcenverwaltung. Die Uebersicht verbindet Runtime-Abwehr, Host-/Container-Schutz, Komponentenvertrauen, Identitaet, Richtlinien, Kollaborations-Governance, Notfallreaktion und Sicherheitsereignisse.

![ClawManager Security Protection](./main/security-protection-current.png)

## Uebersicht und Aktionen

Die vier Kennzahlen zeigen heutige Treffer, High-Severity-Ereignisse der letzten 24 Stunden, blockierte/abgelehnte Ereignisse und betroffene Agent-Instanzen. Alerts werden automatisch aktualisiert; die zehn neuesten Ereignisse zeigen Zeit, Quelle, Szenario, Evidenz, Ziel und Schweregrad.

- **Pod Live Aegis Configuration** oeffnet die Runtime-Sicherheitskonfiguration und deren Dispatch-Ablauf.
- **Report exportieren** laedt die aktuell geladenen Alerts als JSON Lines herunter.
- **Emergency Circuit Breaker** verlangt Begruendung und Bestaetigung, verteilt danach den Notfallstatus und zeigt bei Aktivierung Autor, Zeitpunkt und Grund.

Vor Live-Konfiguration oder Circuit Breaker immer Zielbereich und Auswirkungen bestaetigen.

## KSecure-Modell

Die UI beschreibt das Modell als **7 Risikoflaechen, 15 Schutzszenarien und 4 Verteidigungsschichten** und bietet Schichten- und Ringansicht.

- **Runtime-Schicht**: Eingabe, Zustand/Memory, Entscheidungen und Tool Calls, Ausgabe, Asset-Schutz, Human Approval.
- **Host-Schicht**: Host-Haertung und Container-Isolation.
- **Audit-Schicht**: Skill Scanner sowie kontrollierte private Egress-Ausnahmen.
- **Control-Schicht**: Outbound- und Identitaets-Governance, Policy-Templates, Circuit Breaker, Full-Chain-Audit, Team-Kollaboration und AI-Gateway-Quotas.

Karten verlinken auf die jeweiligen Szenarioseiten. Sichtbare Eintraege garantieren nicht, dass jede Backend-Durchsetzung aktiv ist; dies haengt von bereitgestellten Security Services und Runtime Agents ab.

## Vorgehen und Grenzen

Ereignis und betroffenes Ziel identifizieren, das zugehoerige Szenario oeffnen, die kleinstmoegliche Massnahme anwenden und danach neue Events sowie Runtime-Zustand pruefen. Den Circuit Breaker nur bei gerechtfertigter Unterbrechung einsetzen.

Resource Management verwaltet Channels, Skills, Tasks, Pakete und Injection Records. Skill Scanner ist ein Szenario innerhalb von Security Protection: Benutzer sehen Upload, Scanstatus und Report in Skill Hub; Administratoren pruefen hier Scanner-Health, fehlgeschlagene Jobs, Modell/Meta-LLM, Quick/Deep-Policy und Security Events. Ein abgeschlossener Scan ist keine automatische Freigabe. Die Plattform ersetzt nicht Kubernetes-Haertung, Network Policies, Credential-Hygiene, Backups oder organisatorische Incident Response.

Siehe [Skill Hub](./skill-hub-guide_de.md), [Ressourcenverwaltung](./resource-management_de.md), [AI Gateway](./aigateway_de.md) und [Benutzerhandbuch](./use_guide_de.md).
