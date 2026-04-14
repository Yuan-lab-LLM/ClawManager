[<- Zurueck zur README-Startseite](../README.de.md)

# ClawManager Bereitstellungs- und Schnellstartanleitung

## Inhaltsverzeichnis
- [I. Umgebung und Ziel](#sec-01)
- [II. Überblick über die Bereitstellungsarten](#sec-02)
- [III. Variante A: Bereitstellung mit k3s](#sec-03)
- [IV. Variante B: Bereitstellung mit Standard-Kubernetes](#sec-04)
- [V. Empfehlungen zum Ziehen von Images in Netzwerken auf dem chinesischen Festland (optional)](#sec-05)
- [VI. ClawManager bereitstellen](#sec-06)
- [VII. Weboberfläche starten](#sec-08)
- [VIII. Schnellstartanleitung (nach dem Login initialisieren und eine OpenClaw-Instanz erstellen)](#sec-09)
- [IX. Konsole und weitere Funktionen des AI Gateway](#sec-12)
- [X. Beschreibung des Workspace-Moduls](#sec-13)
- [XI. Schnelle Referenz für Probleme und Gegenmaßnahmen](#sec-14)
- [XII. Empfohlene abschließende Prüfreihenfolge (zur Selbstprüfung)](#sec-15)

<a id="sec-01"></a>
## I. Umgebung und Ziel
- **Systemannahme**: Linux-Server mit `x86_64`-Architektur.
- **Bereitstellungsziel**: **ClawManager** bereitstellen, die Konfiguration des Sicherheitsmodells auf der Weboberfläche abschließen und anschließend eine **OpenClaw Desktop**-Instanz erstellen und starten.
- **Geeignete Szenarien**:
  - **Variante A: k3s-Einzelknoten-/Lightweight-Cluster-Bereitstellung**
  - **Variante B: Standard-Kubernetes-Cluster-Bereitstellung** (z. B. kubeadm-Cluster, Enterprise-K8s-Cluster, Kubernetes-Cluster in der Cloud)


---

<a id="sec-02"></a>
## II. Überblick über die Bereitstellungsarten
Sie können mit einer der folgenden zwei Methoden bereitstellen:

### Variante A: k3s-Bereitstellung
Geeignet für Einzelknoten, Testumgebungen oder leichte Produktionsumgebungen.

### Variante B: Standard-Kubernetes-Bereitstellung
Geeignet für Serverumgebungen, die bereits über einen Standard-Kubernetes-Cluster verfügen.

Unabhängig davon, welche Methode Sie verwenden, wird am Ende dasselbe ClawManager-Manifest angewendet:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
```

---

<a id="sec-03"></a>
## III. Variante A: Bereitstellung mit k3s

### 3.1 k3s installieren
```bash
curl -sfL https://get.k3s.io | sh -
```

In Netzwerken auf dem chinesischen Festland kann die Installation über eine Mirror-Quelle erfolgen:

```bash
curl -sfL https://rancher-mirror.rancher.cn/k3s/k3s-install.sh |   INSTALL_K3S_MIRROR=cn sh -
```

### 3.2 Dienststatus prüfen
```bash
sudo systemctl status k3s --no-pager
sudo systemctl enable k3s
```

### 3.3 kubectl konfigurieren
Wenn der aktuelle Benutzer `kubectl` nicht direkt verwenden kann, führen Sie Folgendes aus:

```bash
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown "$USER:$USER" ~/.kube/config
```

Oder geben Sie es temporär an:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

### 3.4 Cluster überprüfen
```bash
kubectl get nodes
```

Normalerweise sollte der Node den Status `Ready` haben.

---

<a id="sec-04"></a>
## IV. Variante B: Bereitstellung mit Standard-Kubernetes

> Gilt für x86-Serverumgebungen, in denen bereits ein nutzbarer Kubernetes-Cluster vorhanden ist.

### 4.1 Voraussetzungen prüfen
Vergewissern Sie sich, dass das aktuelle `kubectl` mit dem Ziel-Cluster verbunden ist:

```bash
kubectl get nodes
kubectl get ns
```

Normalerweise sollte mindestens ein `Ready`-Node angezeigt werden.

### 4.2 Standard-StorageClass prüfen
MySQL und MinIO in ClawManager benötigen persistente Speicherung. Es wird empfohlen, zunächst zu prüfen, ob im Cluster eine Standard-`StorageClass` vorhanden ist:

```bash
kubectl get storageclass
```

Wenn im Cluster bereits eine Standard-StorageClass vorhanden ist, können Sie mit der Bereitstellung direkt fortfahren.

Wenn **keine Standard-StorageClass** vorhanden ist, wird empfohlen, im Voraus nutzbare PV / PVC bereitzustellen oder eine lokale Pfad-Storage-Lösung zu verwenden. Andernfalls kann später Folgendes auftreten:

```text
pod has unbound immediate PersistentVolumeClaims
```

---

<a id="sec-05"></a>
## V. Empfehlungen zum Ziehen von Images in Netzwerken auf dem chinesischen Festland (optional)
Wenn der Server Docker Hub oder andere öffentliche Registries nur langsam erreicht, können Sie Image-Beschleunigung konfigurieren.

### 5.1 k3s-Szenario: `/etc/rancher/k3s/registries.yaml` konfigurieren
```yaml
mirrors:
  docker.io:
    endpoint:
      - "https://docker.m.daocloud.io"
      - "https://docker.nju.edu.cn"
      - "https://docker.1ms.run"
  quay.io:
    endpoint:
      - "https://quay.mirrors.ustc.edu.cn"
  gcr.io:
    endpoint:
      - "https://gcr.mirrors.ustc.edu.cn"
  k8s.gcr.io:
    endpoint:
      - "https://registry.aliyuncs.com/google_containers"
```

Führen Sie nach der Änderung Folgendes aus:

```bash
sudo systemctl restart k3s
```

### 5.2 Image-Pull verifizieren
```bash
sudo k3s crictl pull docker.io/rancher/mirrored-pause:3.6
```

---

<a id="sec-06"></a>
## VI. ClawManager bereitstellen

### 6.1 Projektcode abrufen
```bash
git clone https://github.com/Yuan-lab-LLM/ClawManager.git
cd ClawManager
```

### 6.2 Bereitstellungsmanifest anwenden
Führen Sie im Wurzelverzeichnis des Repositorys aus:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
```

### 6.3 Basisressourcen prüfen
```bash
kubectl get ns
kubectl get pods -n clawmanager-system
kubectl get svc -n clawmanager-system
```

Unter normalen Umständen sehen Sie die folgenden Komponenten:
- `clawmanager-app`
- `mysql`
- `minio`
- `skill-scanner`

Wenn Sie den folgenden Fehler sehen:

```text
0/1 nodes are available: pod has unbound immediate PersistentVolumeClaims
```

bedeutet dies, dass MySQL / MinIO im Cluster-Speicher nicht starten können, weil die PVC nicht gebunden ist. Springen Sie bitte direkt ans Ende des Dokuments:

- [XI.1 Spezielle Behandlung von Speicherproblemen (PV/PVC)](#sec-14-storage)

---

<a id="sec-08"></a>
## VII. Weboberfläche starten

### 7.1 Zugriff über NodePort
Der ClawManager-Frontend-Service verwendet standardmäßig einen HTTPS-NodePort. Prüfen Sie zunächst:

```bash
kubectl get svc -n clawmanager-system
```

Wenn der Frontend-Port wie folgt ist:

```text
443:30443/TCP
```

können Sie ihn direkt im Browser aufrufen:

```text
https://<Server-IP>:30443
```


### 7.2 Hinweis zum ersten HTTPS-Zugriff
Da in der Regel ein selbstsigniertes Zertifikat verwendet wird, zeigt der Browser möglicherweise „Unsicher“ oder eine Zertifikatswarnung an. Klicken Sie auf:

```text
Erweitert → Trotzdem fortfahren
```

um die Seite zu öffnen.

---

<a id="sec-09"></a>
## VIII. Schnellstartanleitung (nach dem Login initialisieren und eine OpenClaw-Instanz erstellen)

Nachdem Sie die obige Bereitstellung abgeschlossen und die Verwaltungsseite erfolgreich geöffnet haben, müssen Sie noch die folgenden Initialisierungsschritte durchführen, bevor Sie tatsächlich eine **OpenClaw**-Instanz erstellen und starten können.

### 8.1 Im System anmelden
1. Öffnen Sie die Seite nach der Bereitstellung, z. B.: `https://<Knoten-IP>:30443`.
2. Melden Sie sich mit dem Standard-Administratorkonto an:
   - **Benutzername**: `admin`
   - **Passwort**: `admin123`
3. Nach der ersten Anmeldung wird empfohlen, das Standardpasswort nach Bedarf zu ändern.


### 8.2 Sicherheitsmodell konfigurieren (AI Gateway)

![Abbildung 1: AI-Gateway-Konfiguration](./main/1.png)
Nach dem Login müssen Sie zunächst ein verfügbares **Sicherheitsmodell** konfigurieren, das von der Plattform und von nachfolgenden Instanzen gemeinsam genutzt wird.

1. Klicken Sie im linken Menü auf **AI Gateway** → **Modelle**.
2. Fügen Sie ein neues Modell hinzu oder bearbeiten Sie ein bestehendes Modell und tragen Sie die folgenden Informationen entsprechend dem tatsächlich angebundenen Modelldienst ein:

   * **Anzeigename**: Geben Sie einen leicht erkennbaren Namen ein.
   * **Anbietervorlage**: Wählen Sie die passende Vorlage entsprechend dem Typ Ihres Modelldienstes; wenn Sie eine benutzerdefinierte oder kompatible Schnittstelle verwenden, können Sie **Local / Internal** auswählen.
   * **Protokoll**: Wählen Sie das Protokoll entsprechend der Schnittstelle, z. B. **OpenAI Compatible** oder ein anderes tatsächlich verwendetes Protokoll.
   * **Base URL**: Tragen Sie die vom Modelldienst bereitgestellte Schnittstellenadresse ein.
   * **API Key**: Tragen Sie den gültigen Schlüssel des entsprechenden Modelldienstes ein.
   * **Provider Model**: Tragen Sie den tatsächlichen Namen des aufzurufenden Modells ein.
   * **Währung**: Tragen Sie die Angabe entsprechend Ihrer tatsächlichen Situation ein; wenn keine Kostendarstellung benötigt wird, können Sie den Standardwert beibehalten.
   * **Eingabepreis / Ausgabepreis**: Wenn keine Kostenstatistik benötigt wird, können Sie zunächst `0` eintragen.
3. Aktivieren Sie vor dem Absenden unbedingt:

   * **Sicherheitsmodell**
   * **Aktiviert**
4. Klicken Sie auf **Speichern**.

> Hinweis: Die Bilder auf der Seite dienen nur dazu, die Positionen der Eingabefelder und das Beispiel-Format zu zeigen. Die tatsächlichen Inhalte sollten sich nach der Konfiguration des von Ihnen verwendeten Modelldienstes richten.


### 8.3 OpenClaw-Instanz erstellen
Nach Abschluss der Modellkonfiguration erstellen Sie eine **OpenClaw Desktop**-Instanz.

1. Klicken Sie unten links auf **ADMIN** und wechseln Sie in den **Workspace**.
2. Klicken Sie auf **Instanz erstellen**.

![](./main/2.png)
#### Schritt 1: Grundinformationen
- Geben Sie den **Instanznamen** ein (mindestens 3 Zeichen).
- Die Beschreibung ist optional und kann leer bleiben.
- Klicken Sie auf **Weiter**.

![](./main/3.png)
#### Schritt 2: Typ auswählen
- Wählen Sie **OpenClaw Desktop** aus.
- Klicken Sie auf **Weiter**.


![](./main/4.png)
#### Schritt 3: Konfiguration
- Sie können direkt die Spezifikation **Small** auswählen:
  - `2 CPU`
  - `4 GB RAM`
  - `20 GB Disk`
- Sie können die Einstellungen auch im darunterliegenden benutzerdefinierten Konfigurationsbereich nach Bedarf anpassen.
- Im Bereich für OpenClaw-Ressourceneinbindung können Sie je nach Bedarf auswählen:
  - **Manuelle Ressourcen**
  - **Ressourcenpaket**
  - **Archivimport**
- Bei der ersten Verwendung können Sie die Standardwerte beibehalten oder **Manuelle Ressourcen** auswählen.
- Klicken Sie abschließend auf **Erstellen**.

### 8.4 Hinweis zur ersten Erstellung
- Wenn Sie zum ersten Mal eine **OpenClaw**-Instanz erstellen, müssen die erforderlichen Images heruntergeladen und die Umgebung initialisiert werden, daher dauert es deutlich länger.
- Bei langsamen Netzwerken oder beim ersten Image-Pull kann der Instanzstatus lange als **Erstellen** angezeigt werden. Bitte warten Sie geduldig.
- Wenn der Start auch nach längerer Zeit nicht erfolgreich ist, gehen Sie zurück zu den Kubernetes-/Docker-Logs und prüfen Sie Image-, PVC-, Gateway-Modell- und andere Probleme.

---

<a id="sec-12"></a>
## IX. Konsole und weitere Funktionen des AI Gateway

Neben der Modellkonfiguration bieten die Konsole auf der Startseite der Plattform und das AI Gateway auch Funktionen für Auditierung, Kosten und Regelverwaltung. So können Administratoren den Clusterstatus, Modellaufrufe und die Ausführung von Sicherheitsrichtlinien zentral einsehen.

### 9.1 Konsolenübersicht

![](./main/5.png)

Die Startseite der Konsole dient dazu, den gesamten Betriebszustand des aktuellen Clusters und der Plattform anzuzeigen, damit Administratoren Ressourcennutzung und Systemzustand schnell erfassen können.

Sie umfasst hauptsächlich die folgenden Informationen:

- **Übersicht über grundlegende Clusterinformationen**: Zeigt die Gesamtzahl der Benutzer, die Gesamtzahl der Instanzen, die Anzahl laufender Instanzen und die gesamte Speichernutzung der aktuellen Plattform an.
- **Knotenübersicht**: Zeigt die aktuelle Anzahl verfügbarer Knoten sowie die wichtigsten Scheduling-Knoteninformationen im aktuellen Cluster an.
- **Ressourcenanforderungsstatus**: Zeigt die Gesamtmenge der aktuell von der Plattform angeforderten CPU-, Speicher- und Plattenressourcen an.
- **Kapazitäts-Dashboard**: Zeigt die Gesamtkapazität und aktuelle Auslastung von Knoten, CPU, Speicher, Festplatte und weiteren Dimensionen an, damit leicht beurteilt werden kann, ob im Cluster noch freie Kapazität vorhanden ist.
- **Infrastruktur-Tabelle**: Dient zur Anzeige des Status aktueller Knoten, Ressourcen und der grundlegenden Laufzeitumgebung.

> Hinweis: Die Konsole dient hauptsächlich dazu, die Gesamtressourcen der Plattform, die Knoten und den Betriebsüberblick über Instanzen anzuzeigen, und wird nicht direkt für konkrete OpenClaw-Operationen innerhalb einzelner Instanzen verwendet.

### 9.2 Sicherheitszentrum (skill-scanner)

Das **Sicherheitszentrum** in der Konsole dient dazu, den Scan-Status der Plattformressourcen, historische Berichte und Scanner-Konfigurationen zentral einzusehen. Es basiert auf dem Backend-Dienst **skill-scanner** und kann statische Scans, Deep Scans sowie ergänzende, LLM-basierte Analysen auf Ressourcen ausführen. Dadurch unterstützt es Administratoren dabei, potenziell riskante Inhalte, ungewöhnliche Ressourcen und verdächtige Skills zu identifizieren.

Das Sicherheitszentrum umfasst derzeit hauptsächlich die folgenden drei Module:

* **Laufzeitübersicht**
* **Berichtshistorie**
* **Scanner-Konfiguration**

#### 9.2.1 Laufzeitübersicht

![](./main/14.png)

Die Seite „Laufzeitübersicht“ dient dazu, den gesamten aktuellen Scan-Status und die Risikoverteilung der Plattform einzusehen, damit Administratoren die aktuelle Sicherheitslage schnell erfassen können.

Die Seite enthält hauptsächlich die folgenden Informationen:

* **Aktuell wirksamer Modus**: Zeigt an, ob derzeit der **Quick-Modus** oder der **Deep-Modus** verwendet wird.
* **Schnellscan / Vollscan**:

  * **Schnellscan**: Geeignet für neu hinzugefügte oder geänderte Ressourcen. Der Scanumfang ist leichter und die Ausführung schneller.
  * **Vollscan**: Geeignet für das regelmäßige erneute Scannen aller Ressourcen, um den aktuellen Zustand aller Plattformressourcen vollständig zu überprüfen.
* **Gesamtzahl der Assets**: Anzahl der Ressourcen, die aktuell in den Scanbereich des Sicherheitszentrums aufgenommen sind.
* **Abgeschlossene Scans**: Anzahl der Ressourcen, deren Scan bereits abgeschlossen ist.
* **Hohes Risiko / Mittleres Risiko**: Statistik der in den aktuellen Scanergebnissen erkannten Risikostufen.
* **Scan-Abdeckung**: Zeigt den Anteil der tatsächlich gescannten Assets an der Gesamtzahl der Plattform-Assets.
* **SAFE / Hohes Risiko / Wartend / Fehlgeschlagen**:

  * **SAFE**: Anzahl der Ressourcen, die den Scan bestanden haben und bei denen aktuell kein Risiko festgestellt wurde
  * **Hohes Risiko**: Anzahl der Risiko-Assets, die sofort bearbeitet werden müssen
  * **Wartend**: Anzahl der Ressourcen, die auf Beweissicherung oder auf die Einreihung in die Scan-Warteschlange warten
  * **Fehlgeschlagen**: Anzahl der fehlgeschlagenen Scan-Ausführungen, die erneut ausgeführt werden müssen
* **Risikolage der Plattform-Assets**: Zeigt die aktuelle Risikoverteilung der Plattform-Assets aggregiert nach Risikostufe.
* **Hot Assets**: Zeigt die am häufigsten verwendeten Skills oder hochfrequent genutzten Ressourcen an, damit Administratoren wichtige Assets schnell identifizieren können.
* **Scanner-Status**: Zeigt die Verfügbarkeit und den Verbindungsstatus des aktuellen skill-scanner an, zum Beispiel „Statischer Scan verfügbar“ oder „Verbunden“.
* **Risikohinweise und Handlungsempfehlungen**: Gibt kurze Hinweise entsprechend der aktuellen Risikolage.
* **Letzte Scan-Aufgaben**: Zeigt kürzlich ausgeführte Scan-Einträge an, damit aktuelle Scan-Aktivitäten nachvollzogen werden können.

> Hinweis:
>
> * Wenn auf der Seite „Derzeit gibt es keine Assets mit hohem oder mittlerem Risiko“ angezeigt wird, bedeutet dies, dass in den aktuellen Scan-Ergebnissen keine signifikanten Risiken gefunden wurden.
> * Wenn auf der Seite „Es gibt noch keine Scan-Aufzeichnungen“ angezeigt wird, bedeutet dies, dass bisher noch kein Scan ausgeführt wurde oder noch keine gültigen Scan-Ergebnisse erzeugt wurden.

#### 9.2.2 Berichtshistorie

Die Seite „Berichtshistorie“ dient dazu, historische Scan-Berichte und zugehörige Ergebnisdatensätze einzusehen, damit Administratoren vergangene Scan-Ausführungen nachvollziehen können.

Dieses Modul dient hauptsächlich dazu:

* Ergebnisse bereits ausgeführter Scan-Aufgaben einzusehen
* Scan-Ausgaben zu verschiedenen Zeitpunkten zu vergleichen
* Änderungen des Sicherheitszustands einer bestimmten Ressource über verschiedene Phasen hinweg nachzuverfolgen
* Eine historische Grundlage für spätere Prüfungen, erneute Scans und Fehleranalysen bereitzustellen

> Hinweis:
>
> * Die „Berichtshistorie“ ist stärker auf die Archivierung und Rückverfolgung historischer Ergebnisse ausgerichtet；
> * Die „Laufzeitübersicht“ ist stärker auf den aktuellen Zustand und den Gesamtüberblick ausgerichtet。

#### 9.2.3 Scanner-Konfiguration

![](./main/15.png)

Die Seite „Scanner-Konfiguration“ dient zur Verwaltung der Betriebsweise von skill-scanner, der LLM-bezogenen Einstellungen sowie der beiden Scan-Strategien quick und deep. Nach dem Speichern wird ein Deployment-Rollout ausgelöst und auf das Wirksamwerden der neuen Konfiguration gewartet.

Die Seite enthält hauptsächlich die folgenden Inhalte:

##### (1) skill-scanner Servicestatus

* Zeigt Namespace, Deployment-Namen und Verbindungsstatus des aktuellen Backend-Scandienstes an.
* Wenn auf der Seite **Verbunden** und **Statischer Scan verfügbar** angezeigt wird, bedeutet dies, dass die grundlegende statische Scan-Funktion verfügbar ist.

##### (2) LLM-Konfiguration

Dieser Bereich dient zur Konfiguration des primären LLM, damit der Scanner bei Bedarf modellbasierte Analysen ausführen kann.

Die wichtigsten Felder sind:

* **Primäre LLM-Integration**: Die Konfiguration des primären LLM kann direkt aus einem bereits in **AI Gateway** konfigurierten Modell importiert werden.
* **LLM API Key**: Entspricht `SKILL_SCANNER_LLM_API_KEY` und wird zur Authentifizierung des primären LLM analyzers verwendet.
* **LLM Model**: Entspricht `SKILL_SCANNER_LLM_MODEL`, zum Beispiel ein konkreter Modellname.
* **LLM Base URL**: Entspricht `SKILL_SCANNER_LLM_BASE_URL` und dient zur Konfiguration der Serviceadresse des primären LLM.

##### (3) Meta-LLM-Integration

Dieser Bereich dient zur Konfiguration des Modells, das vom Meta Analyzer verwendet wird. Es wird typischerweise für die weitere Zusammenfassung, Konsolidierung oder sekundäre Verarbeitung von Findings verwendet.

Die wichtigsten Felder sind:

* **Meta-LLM-Integration**: Die Konfiguration des Meta Analyzers kann direkt aus einem bereits in **AI Gateway** konfigurierten Modell importiert werden.
* **Meta LLM API Key**: Entspricht `SKILL_SCANNER_META_LLM_API_KEY`.
* **Meta LLM Model**: Entspricht `SKILL_SCANNER_META_LLM_MODEL`.
* **Meta LLM Base URL**: Entspricht `SKILL_SCANNER_META_LLM_BASE_URL`.

> Hinweis:
>
> * Wenn derzeit kein LLM konfiguriert ist, zeigt die Seite in der Regel an, dass aktuell nur statisches Scannen unterstützt wird；
> * Erst nach der Konfiguration des primären LLM und des Meta LLM kann der Scanner vollständigere semantische Analysen und Zusammenfassungen aktivieren。

##### (4) Aktueller Scan-Modus

Die Seite unterstützt die Auswahl des aktuell von der Plattform verwendeten Scan-Modus:

* **Quick-Modus**: Verwendet quick analyzers für den Scan und eignet sich für tägliche Schnellprüfungen.
* **Deep-Modus**: Verwendet deep analyzers für den Scan und eignet sich für vollständigere und tiefere Analysen.

Wichtig ist:

* Sowohl „Schnellscan“ als auch „Vollscan“ im Dashboard verwenden die hier ausgewählte Scan-Stärke；
* Der Unterschied liegt hauptsächlich im Scan-Umfang und nicht in der Tiefe der Analyzer selbst。

##### (5) Quick / Deep Scan-Strategie

Im unteren Bereich der Seite werden die beiden Scan-Strategie-Konfigurationen **Quick** und **Deep** separat gepflegt, damit Administratoren je nach Szenario unterschiedliche Analyzer-Kombinationen auswählen können.

Jede Strategie umfasst die folgenden Konfigurationseinträge:

* **Timeout (Sekunden)**: Legt die Timeout-Zeit für Scan-Aufgaben im aktuellen Modus fest.
* **Aufrufmethoden**: Verschiedene Analyzer können je nach Bedarf aktiviert oder deaktiviert werden.

Die derzeit sichtbaren Analyzer-Typen umfassen:

* **Static**: YAML + YARA statisches Regel-Scannen
* **Bytecode**: Integritätsprüfung von Python-Bytecode
* **Pipeline**: Befehlsketten- und Taint-Analyse
* **Behavioral**: AST-basierte Verhaltens- und Datenflussanalyse
* **LLM**: Semantische Analyse auf Basis externer LLMs
* **Meta**: Sekundäre Zusammenfassungsanalyse von Findings

Dies kann in der Regel wie folgt verstanden werden:

* **Quick-Modus**: Legt den Schwerpunkt auf schnellere Ausführung und wird häufig für tägliche inkrementelle Prüfungen verwendet
* **Deep-Modus**: Kann mehr Analyzer aktivieren und eignet sich für tiefere Prüfungen und Sicherheits-Audits

##### (6) Speichern und anwenden

Die Schaltfläche **Speichern und anwenden** oben rechts dient dazu, alle aktuellen scanner-bezogenen Konfigurationen zu übernehmen. Nach dem Speichern werden folgende Aktionen ausgeführt:

* Aktualisierung der quick / deep Scan-Strategien in ClawManager
* Aktualisierung der relevanten Umgebungsvariablen des skill-scanner Deployment
* Warten auf den Abschluss des Rollouts, bevor die neue Konfiguration offiziell wirksam wird

> Hinweis:
>
> * Nach Änderungen an der Scanner-Konfiguration wird empfohlen, vor dem Start neuer Scan-Aufgaben zu warten, bis die Konfiguration vollständig wirksam ist；
> * Falls der Verbindungsstatus nach der Konfiguration ungewöhnlich ist, sollten zuerst das AI Gateway-Modell, die LLM-Adresse, der Key und der Deployment-Rollout-Status geprüft werden。

### 9.3 Überblick über die AI-Gateway-Funktionen

Zusätzlich zur Konfiguration von „Modellen“ enthält AI Gateway auch die folgenden Module:

* **AI Audit**: Zeigt Modellaufruf-Traces, Request- und Response-Payloads, erkannte Risiken, Routing-Entscheidungen und Aufrufdetails an.
* **Kosten**: Zeigt Token-Nutzung, geschätzte Kosten, interne Kosten und Trendstatistiken an.
* **Risikokontrollregeln**: Konfiguriert Regeln zur Erkennung sensibler Inhalte und steuert, ob Treffer freigegeben oder an das Sicherheitsmodell weitergeleitet werden.

### 9.4 Kostenmodul

Die Kostenseite dient dazu, die Kosten und die Token-Nutzung von Modellaufrufen auf der Plattform zu erfassen und Administratoren beim Verständnis des Gesamtverbrauchs zu unterstützen.

![](./main/6.png)

Die Seite enthält hauptsächlich die folgenden Inhalte:

* **Input Token**: Statistik über die Gesamtmenge der Eingabe-Prompts
* **Output Token**: Statistik über die Gesamtmenge der vom Modell generierten Inhalte
* **Geschätzte Kosten**: Auf Basis der Provider-Stückpreise geschätzte Kosten
* **Interne Kosten**: Interne Verrechnungskosten im Zusammenhang mit dem Sicherheitsmodell
* **Täglicher Kostentrend**: Zeigt die Veränderungen von geschätzten Kosten und Token im aktuellen Fenster über die letzten 7 Tage an
* **Benutzerübersicht**: Aggregierte Nutzung und Kosten nach Benutzer
* **Instanzübersicht**: Aggregierte Nutzung und Kosten nach Instanz
* **Neueste Kostenaufzeichnungen**: Unterstützt Suche und Paginierung von Kostenaufzeichnungen nach Trace, Benutzer, Modell und weiteren Bedingungen und ermöglicht den Sprung zu Audit-Details

> Hinweis: Falls noch keine Modellaufruf-Datensätze erzeugt wurden, können Input Token, Output Token, Kosten und Trenddiagramme alle 0 sein. Das ist normal.

### 9.5 AI-Audit-Modul

Die AI-Audit-Seite dient dazu, kürzliche Aufrufdatensätze verwalteter Modelle einzusehen und Administratoren bei der Untersuchung von Modellaufrufen, Token-Nutzung und Routing-Ergebnissen zu unterstützen.

![](./main/7.png)

Die Hauptfunktionen umfassen:

* **Letzte AI Trace**: Zeigt aktuelle Modellaufruf-Ketten an
* **Trace-Liste**: Zeigt aktuelle verwaltete Traces in einer einheitlichen Tabelle an
* **Suche und Filterung**: Unterstützt Suche nach Trace, Request-Inhalt, Benutzer, Modell und weiteren Bedingungen
* **Statusfilterung**: Unterstützt die Anzeige verschiedener Aufrufergebnisse nach Status
* **Modellfilterung**: Unterstützt die Filterung zugehöriger Aufrufdatensätze nach Modell
* **Paginierung und Aktualisierung**: Unterstützt paginierte Anzeige und manuelles Aktualisieren der neuesten Audit-Ergebnisse

> Hinweis: Wenn auf der Seite „Es liegen noch keine AI-Audit-Aufzeichnungen vor“ angezeigt wird, bedeutet dies, dass noch keine tatsächlichen Modellaufruf-Anfragen erzeugt wurden.

### 9.6 Modul für Risikokontrollregeln

Die Seite für Risikokontrollregeln dient dazu, Erkennungsregeln für sensible Inhalte zu konfigurieren und festzulegen, welche Aktion nach einem Regeltreffer ausgeführt werden soll.

![](./main/8.png)

Dieses Modul unterstützt hauptsächlich:

* **Verwaltung der Regelliste**: Anzeige aller Regeln und ihres Aktivierungsstatus
* **Ansicht nach Regelkategorie**: Unterstützt die Anzeige nach Kategorien wie personenbezogene Informationen, Unternehmensinformationen, Kundengeschäft, Sicherheitszugangsdaten, Finanzen und Recht, politisch sensible Inhalte und benutzerdefiniert
* **Konfiguration der Regelfelder**: Es können Regel-ID, Anzeigename, Schweregrad, Aktion, Reihenfolge, Regex-Pattern und Beschreibung festgelegt werden
* **Steuerung der Regelaktion**: Bei einem Treffer kann gewählt werden, ob Inhalte freigegeben oder an das Sicherheitsmodell weitergeleitet werden
* **Stapelweises Aktivieren / Deaktivieren**: Unterstützt die stapelweise Anpassung des Regelstatus
* **Regel-Testkonsole**: Ermöglicht das Einfügen von Beispieltexten, um zu testen, welche aktiven oder Entwurfsregeln ausgelöst werden

Die aktuell integrierten Regelbeispiele umfassen unter anderem:

* Personenbezogene Informationen: E-Mail-Adresse, Mobiltelefonnummer, Ausweisnummer, Reisepassnummer, Bankkartenkontext, Adresse, Lebenslaufinhalte usw.
* Unternehmensinformationen: interne IP, interne Domain, Host-Benennung, Kubernetes Service DNS, Projekt-Codename, Organisationsstruktur, Gehalts- / HR-Informationen usw.
* Kundengeschäft: Kundenlisten, Verträge / Angebote, Steuer-IDs auf Rechnungen, CRM- / Ticket-Daten usw.
* Sicherheitszugangsdaten: Private Keys, API Keys, Tokens, JWT, Cookie / Session, Datenbank-Verbindungsstrings, Kubeconfig, geheime Umgebungsvariablen usw.
* Finanzen und Recht: Budget, Gewinn, Umsatz, Rechtsgutachten, Rechtsstreitigkeiten, NDA usw.
* Politisch sensible Inhalte: politische Institutionen, Militär / nationale Sicherheit, extremistische und gewaltbezogene Ausdrücke usw.

> Hinweis: Die Standardregeln decken bereits viele gängige Szenarien zur Erkennung sensibler Informationen ab. In der Praxis können Regeln je nach Geschäftsanforderung weiter ergänzt, angepasst oder deaktiviert werden.
---

<a id="sec-13"></a>
## X. Beschreibung des Workspace-Moduls

Der Workspace ist der wichtigste Arbeitsbereich, nachdem sich ein normaler Benutzer bei der Plattform angemeldet hat. Er wird verwendet, um persönliche Ressourcenquoten einzusehen, Instanzen zu erstellen, Instanzen zu verwalten und OpenClaw-bezogene Ressourcen zu pflegen. Dieses Modul ist stärker auf tägliche Nutzung und Betriebsaufgaben ausgerichtet als die administratorseitige „Konsolenübersicht“.

### 10.1 Workspace-Startseite
![](./main/9.png)
Die Workspace-Startseite dient dazu, die Übersicht über Instanzen und Ressourcennutzung des aktuellen Kontos anzuzeigen und umfasst hauptsächlich die folgenden Inhalte:

- **Meine Instanzen**: Zeigt die Anzahl der unter dem aktuellen Konto erstellten Instanzen an.
- **Laufend**: Zeigt die Anzahl der aktuell laufenden Instanzen an.
- **Verwendeter Speicher**: Zeigt den derzeit vom Konto belegten Speicherplatz an.
- **Meine Ressourcenquoten**: Zeigt die für das aktuelle Konto verfügbaren Quoten an, darunter Anzahl der Instanzen, maximale CPU-Kerne, maximaler Speicher, maximaler Storage und maximale GPU-Anzahl.
- **Schnellaktionen**: Bietet zwei Einstiege: **Neue Instanz erstellen** und **Alle Instanzen anzeigen**, damit Sie schnell mit der Plattform starten können.

> Hinweis: Wenn auf der Seite „Noch keine Instanzen“ angezeigt wird, können Sie direkt auf **Neue Instanz erstellen** klicken, um mit der Erstellung der ersten OpenClaw Desktop-Instanz zu beginnen.

### 10.2 Meine Instanzen

Die Seite **Meine Instanzen** dient dazu, die unter dem aktuellen Konto erstellten Instanzen zentral anzuzeigen und zu verwalten. Diese Seite übernimmt hauptsächlich die Instanzverwaltungsfunktionen.
![](./main/10.png)
Zu den üblichen unterstützten Aktionen gehören:

- **Instanzstatus anzeigen**: Prüfen, ob sich die Instanz im Status Erstellung, Laufend, Gestoppt oder Fehler befindet.
- **Instanzdetails öffnen**: Grundinformationen, Ressourcenkonfiguration und Laufzeitstatus der Instanz anzeigen.
- **Instanz stoppen**: Wenn die Instanz fehlerhaft läuft oder die Umgebung neu geladen werden muss, kann eine Stop-Aktion ausgeführt werden.
- **Instanz löschen**: Wenn die Instanz nicht mehr benötigt wird, kann sie direkt gelöscht werden, um CPU-, Speicher- und Storage-Ressourcen freizugeben.

> Hinweis: Nach dem Löschen einer Instanz werden die zugehörigen Ressourcen ebenfalls bereinigt. Stellen Sie vor der Ausführung sicher, dass die enthaltenen Daten und Konfigurationen gesichert wurden.

### 10.3 Ressourcenverwaltung

Die Seite **Ressourcenverwaltung** dient dazu, verfügbare OpenClaw-Ressourceninhalte zu pflegen, sodass sie nach dem Start einer Instanz eingebunden und verwendet werden können.
![](./main/11.png)
Die Seite umfasst hauptsächlich die folgenden Bereiche:

- **Ressourcen**: Verfügbare Ressourceneinträge anzeigen und pflegen.
- **Ressourcenpakete**: Mehrere Ressourcen zu wiederverwendbaren Paketen kombinieren, um eine gebündelte Einbindung zu erleichtern.
- **Einbindungsprotokolle**: Verlauf und Ausführungsstatus von Ressourceneinbindungen anzeigen.

Auf der linken Seite der Ressourcenverwaltungsseite können Ressourcen außerdem nach Typ getrennt verwaltet werden. Die derzeit auf der Seite sichtbaren Typen sind:

- **Kanäle**
- **Skills**
- **Agenten (demnächst verfügbar)**
- **Geplante Aufgaben (demnächst verfügbar)**

Rechts oben auf der Seite werden unterstützt:

- **Aktualisieren**: Die aktuelle Ressourcenliste neu laden.
- **Neu**: Einen neuen Ressourceneintrag erstellen.

> Hinweis: Die Ressourcenverwaltung dient hauptsächlich dazu, OpenClaw-Ressourcen vorzubereiten, die nach dem Start einer Instanz verwendet werden können, und ersetzt nicht direkt den Prozess der Instanzerstellung. Bei der Erstellung einer Instanz können Ressourcen über **Manuelle Ressourcen**, **Ressourcenpakete** und **Archivimport** eingebunden werden.


### 10.3.1 Kanal erstellen

„Kanäle“ werden verwendet, um die Verbindungsweise zwischen OpenClaw und externen Nachrichtenplattformen oder Zugriffsendpunkten zu konfigurieren, z. B. Telegram, Slack und Feishu / Lark.

![](./main/12.png)

Gehe beim Erstellen eines Kanals wie folgt vor:

1. Öffne die Seite **Ressourcenverwaltung** und bleibe im Reiter **Ressourcen**.
2. Wähle links unter den Ressourcentypen **Kanal** aus.
3. Klicke rechts auf der Seite auf **Neu**, um das Dialogfenster „Neue Ressource“ zu öffnen.
4. Fülle im Dialog die Basisinformationen aus:
   - **Typ**: **Kanal** auswählen
   - **Ressourcen-Key**: Trage die eindeutige Kennung dieses Kanals ein. Es wird empfohlen, einen leicht erkennbaren und nicht doppelt verwendeten englischen Namen oder eine entsprechende Kombination zu verwenden
   - **Name**: Trage den Anzeigenamen des Kanals ein
   - **Tags**: optional, für Klassifizierung und Suche
   - **Beschreibung**: optional, zur ergänzenden Beschreibung des Kanalzwecks
   - **Aktiviert**: Es wird empfohlen, diese Option aktiviert zu lassen
5. Wähle im Bereich **Channel Template** eine Startvorlage aus. Derzeit werden folgende Vorlagen unterstützt:
   - `Telegram`
   - `Slack`
   - `Feishu / Lark`

6. Nachdem du eine Vorlage ausgewählt hast, klicke auf **Vorlage laden**. Das System schreibt die Grundkonfiguration der entsprechenden Vorlage automatisch in den darunterliegenden Bereich **Content JSON**.
7. Ergänze oder ändere anschließend die Feldinhalte in **Content JSON** entsprechend deinen tatsächlichen Anbindungsinformationen.
8. Wenn die Konfiguration korrekt ist, klicke auf Speichern, um die Erstellung des Kanals abzuschließen.

> Hinweis:
> - **Channel Template** dient dazu, schnell eine Grundkonfiguration zu erzeugen；
> - **Content JSON** ist der tatsächlich wirksame Konfigurationsinhalt des Kanals；
> - Wenn keine Vorlage vollständig passt, kannst du die Konfiguration auch direkt manuell in **Content JSON** eintragen。

### 10.3.2 Skills hochladen

Skills werden verwendet, um OpenClaw wiederverwendbare Funktionsfähigkeiten bereitzustellen. Die Plattform unterstützt den Batch-Import von Skills durch das Hochladen von Archivdateien.

![](./main/13.png)

Gehe beim Hochladen von Skills wie folgt vor:

1. Öffne die Seite **Ressourcenverwaltung** und bleibe im Reiter **Ressourcen**.
2. Wähle links unter den Ressourcentypen **Skills** aus.
3. Klicke auf **Datei auswählen** und wähle ein lokales Skill-Archiv aus.
4. Die aktuelle Seite unterstützt nur das Hochladen von **`.zip`**-Dateien.
5. Nachdem die Datei ausgewählt wurde, klicke rechts auf **Skill-Archiv hochladen**.
6. Das System analysiert den hochgeladenen Inhalt automatisch und importiert jedes Verzeichnis der ersten Ebene als einen Skill.
7. Nach Abschluss des Uploads kannst du die importierten Skills in der Skill-Liste anzeigen.

> Hinweis:
> - Es wird empfohlen, das Skill-Archiv im Voraus sauber nach Verzeichnissen zu strukturieren；
> - Jedes Verzeichnis der ersten Ebene wird als eigenständiger Skill erkannt；
> - Falls die Liste nach dem Upload nicht sofort aktualisiert wird, kannst du oben rechts auf der Seite manuell auf **Aktualisieren** klicken, um neu zu laden。
---

<a id="sec-14"></a>
## XI. Schnelle Referenz für Probleme und Gegenmaßnahmen

<a id="sec-14-storage"></a>
### 11.1 Spezielle Behandlung von Speicherproblemen (PV/PVC)

Wenn der folgende Fehler angezeigt wird:

```text
0/1 nodes are available: pod has unbound immediate PersistentVolumeClaims
```

bedeutet dies, dass der Cluster-Speicher nicht automatisch gebunden wurde. In diesem Fall können Sie lokale `hostPath`-PV/PVC im Stil eines x86-Einzelknotenservers manuell erstellen.

> Diese Lösung eignet sich für Einzelknoten-Servertests oder leichte Umgebungen. Für Produktionsumgebungen wird empfohlen, formelle Speicherlösungen wie NFS, Ceph oder Cloud-Disks zu verwenden.

#### 11.1.1 PV erstellen
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: mysql-pv-local
spec:
  capacity:
    storage: 5Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Delete
  hostPath:
    path: /tmp/mysql-data
EOF

kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: minio-pv-local
spec:
  capacity:
    storage: 10Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Delete
  hostPath:
    path: /tmp/minio-data
EOF
```

#### 11.1.2 PVC erstellen
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mysql-data
  namespace: clawmanager-system
spec:
  storageClassName: ""
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
  volumeName: mysql-pv-local
EOF

kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: minio-data
  namespace: clawmanager-system
spec:
  storageClassName: ""
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
  volumeName: minio-pv-local
EOF
```

#### 11.1.3 Pod neu erstellen
```bash
kubectl delete pod --all -n clawmanager-system
```

#### 11.1.4 Status erneut beobachten
```bash
kubectl get pvc -n clawmanager-system
kubectl get pods -n clawmanager-system -w
```

Erwartetes Ergebnis:
- `mysql-data` / `minio-data` sind `Bound`
- `mysql` / `minio` / `skill-scanner` / `clawmanager-app` sind schließlich `Running`

---

| Symptom | Ursache | Behebung |
| :--- | :--- | :--- |
| Verbindung von `kubectl` zu `localhost:8080` wird abgelehnt | kubeconfig ist nicht konfiguriert | `KUBECONFIG` setzen oder in `~/.kube/config` kopieren |
| Timeout beim Ziehen von Pod-Images | Netzwerk zu Docker Hub / GHCR ist instabil | Image-Beschleunigung oder Proxy konfigurieren |
| MySQL / MinIO bleiben `Pending` | PVC ist nicht gebunden | `StorageClass` prüfen oder PV/PVC manuell erstellen |
| Die Seite lässt sich im Browser nicht öffnen | NodePort ist nicht freigegeben / der `port-forward`-Prozess wurde nicht aufrechterhalten | Port freigeben oder das Weiterleitungs-Terminal geöffnet lassen |
| Die Seite öffnet sich, aber eine OpenClaw-Instanz kann nicht erstellt werden | Sicherheitsmodell ist nicht konfiguriert | Zuerst unter **AI Gateway → Modelle** das Sicherheitsmodell konfigurieren und aktivieren |
| Die Instanz bleibt lange im Status „Erstellen“ | Das erste Image-Pulling dauert lange / Speicher- oder Netzwerkproblem | Geduldig warten und bei Bedarf Pods und Events prüfen |

---

<a id="sec-15"></a>
## XII. Empfohlene abschließende Prüfreihenfolge (zur Selbstprüfung)
1. `kubectl get nodes`
2. `kubectl get storageclass`
3. `kubectl get pods -n clawmanager-system`
4. `kubectl get pvc -n clawmanager-system`
5. `kubectl get svc -n clawmanager-system`
6. Im Browser `https://<IP>:30443` öffnen
7. Im Backend anmelden und die **Konfiguration des Sicherheitsmodells** abschließen
8. Im Workspace eine **OpenClaw Desktop**-Instanz erstellen
