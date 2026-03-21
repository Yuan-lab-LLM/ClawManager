# ClawManager

<p align="center">
  <img src="frontend/public/openclaw_github_logo.png" alt="ClawManager" width="100%" />
</p>

<p align="center">
  Die weltweit erste Plattform, die speziell fuer Batch-Deployment und Betrieb von OpenClaw im Cluster-Massstab entwickelt wurde.
</p>

<p align="center">
  <strong>Sprachen:</strong>
  <a href="./README.md">English</a> |
  <a href="./README.zh-CN.md">中文</a> |
  <a href="./README.ja.md">日本語</a> |
  <a href="./README.ko.md">한국어</a> |
  Deutsch
</p>

<p align="center">
  <img src="https://img.shields.io/badge/ClawManager-Virtual%20Desktop%20Platform-e25544?style=for-the-badge" alt="ClawManager Platform" />
  <img src="https://img.shields.io/badge/Go-1.21%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.21+" />
  <img src="https://img.shields.io/badge/React-19-20232A?style=for-the-badge&logo=react&logoColor=61DAFB" alt="React 19" />
  <img src="https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes Native" />
  <img src="https://img.shields.io/badge/License-MIT-2ea44f?style=for-the-badge" alt="MIT License" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/OpenClaw-Desktop-f97316?style=flat-square&logo=linux&logoColor=white" alt="OpenClaw Desktop" />
  <img src="https://img.shields.io/badge/Webtop-Browser%20Desktop-0f766e?style=flat-square&logo=firefoxbrowser&logoColor=white" alt="Webtop" />
  <img src="https://img.shields.io/badge/Proxy-Secure%20Access-7c3aed?style=flat-square&logo=nginxproxymanager&logoColor=white" alt="Secure Proxy" />
  <img src="https://img.shields.io/badge/WebSocket-Realtime-2563eb?style=flat-square&logo=socketdotio&logoColor=white" alt="WebSocket" />
  <img src="https://img.shields.io/badge/i18n-5%20Languages-db2777?style=flat-square&logo=googletranslate&logoColor=white" alt="5 Languages" />
</p>

## 🚀 News

- [03/20/2026] **ClawManager Neuveröffentlichung** - ClawManager ist jetzt als Virtual-Desktop-Management-Plattform veroeffentlicht und bietet Batch-Deployment, Webtop-Unterstuetzung, Desktop-Portal-Zugriff, Runtime-Image-Einstellungen, OpenClaw-Speicher-/Praeferenz-Markdown-Backup und Migration, Cluster-Ressourcenuebersicht sowie mehrsprachige Dokumentation.

## 👀 Overview

ClawManager ist eine Plattform zur Verwaltung virtueller Desktops auf Kubernetes. Sie bietet eine vollstaendige Kontroll- und Betriebsplattform fuer Desktop-Runtimes, Benutzer-Governance und sicheren In-Cluster-Zugriff.

ClawManager vereint Batch-Deployment, Instanz-Lifecycle-Management, Admin-Konsole, proxybasierten Desktop-Zugriff, Runtime-Image-Steuerung, Cluster-Ressourcen-Transparenz sowie Backup- und Migrationsfunktionen fuer OpenClaw-Speicher und Praeferenzen in einer Plattform.

ClawManager ist fuer Umgebungen gedacht, in denen:

- virtuelle Desktop-Instanzen fuer mehrere Benutzer erstellt und verwaltet werden muessen
- Administratoren Quotas, Images und Instanzen zentral steuern muessen
- Desktop-Dienste innerhalb von Kubernetes bleiben und ueber authentifizierte Proxys bereitgestellt werden sollen
- Betreiber eine einheitliche Sicht auf Instanzzustand, Cluster-Kapazitaet und Runtime-Status benoetigen

Kurz gesagt ist ClawManager:

- eine zentrale Betriebskonsole fuer OpenClaw- und Linux-Desktop-Runtimes
- eine Multi-User-Desktop-Management-Plattform auf Kubernetes
- eine sichere Zugriffsschicht fuer interne Desktop-Dienste ueber token-authentifizierte Proxys

## ✨ At a Glance

- Multi-Tenant-Desktop-Instanzverwaltung
- Batch-Deployment von Desktop-Instanzen ueber Benutzer oder Runtime-Profile hinweg
- Benutzer-Quota-Kontrolle fuer CPU, Speicher, Storage, GPU und Instanzanzahl
- Unterstuetzung fuer OpenClaw, Webtop, Ubuntu, Debian, CentOS und benutzerdefinierte Runtimes
- Sicherer Desktop-Proxy-Zugriff mit Token-Generierung und WebSocket-Weiterleitung
- Backup und Migration von OpenClaw-Speicher-, Praeferenz- und Markdown-Konfigurationsdaten
- Admin-Dashboards fuer Benutzer, Instanzen, Image-Karten und Cluster-Ressourcen
- Mehrsprachige UI: Englisch, Chinesisch, Japanisch, Koreanisch und Deutsch

> 🧭 ClawManager vereint Admin-Kontrolle, sicheren Desktop-Zugriff und Runtime-Operationen in einer Kontrollplattform.

<p align="center">
  <img src="frontend/public/clawmanager_overview.png" alt="ClawManager Overview" width="100%" />
</p>

## 📚 Table of Contents

- [News](#news)
- [Overview](#overview)
- [ClawManager New Features](#clawmanager-new-features)
- [Key Features](#key-features)
- [Typical Workflow](#typical-workflow)
- [Architecture](#architecture)
- [Project Structure](#project-structure)
- [Tech Stack](#tech-stack)
- [Kubernetes Prerequisites](#kubernetes-prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [License](#license)

## 🆕 ClawManager New Features

Dies sind die wichtigsten Funktionen von ClawManager:

- 🖥 `webtop`-Runtime-Unterstuetzung fuer browserbasierten Desktop-Zugriff
- 📦 Batch-Deployment-Funktionen fuer grossflaechige Desktop-Bereitstellung
- 🚪 Desktop-Portal-Seite zum Wechseln zwischen laufenden Instanzen an einem Ort
- 🔐 Tokenbasierter Instanzzugriff und Reverse-Proxy-Routing
- 🔄 WebSocket-Weiterleitung fuer Desktop-Sitzungen und Statusaktualisierungen
- 🧠 Backup-/Import-APIs fuer OpenClaw-Speicher-, Praeferenz- und Markdown-Konfigurationsdaten
- 🧩 Runtime-Image-Kartenverwaltung fuer jeden unterstuetzten Instanztyp
- 📊 Cluster-Ressourcenuebersicht fuer Nodes, CPU, Speicher und Storage
- 👨‍💼 Globale Admin-Instanzverwaltung mit benutzeruebergreifender Filterung und Steuerung
- 📥 CSV-basierter Benutzerimport mit Generierung von Standardpasswoertern
- 🌍 Internationalisiertes Frontend mit 5 Sprachen

## 🛠 Key Features

- ⚙️ Instanz-Lifecycle-Management: erstellen, starten, stoppen, neu starten, loeschen, anzeigen und erzwungen synchronisieren
- 📦 Batch-Deployment-Unterstuetzung fuer gross angelegte Desktop-Rollouts
- 🧱 Unterstuetzte Runtime-Typen: `openclaw`, `webtop`, `ubuntu`, `debian`, `centos`, `custom`
- 🔒 Sicherer Desktop-Zugriff ueber authentifizierte Proxy-Endpunkte
- 📡 WebSocket-basierte Echtzeit-Statusupdates
- 📝 Archiv-Backup/-Import fuer OpenClaw-Speicher-, Praeferenz- und Markdown-Konfigurationsdaten
- 📏 Benutzerbezogene Quota-Verwaltung fuer Instanzen, CPU, Speicher, Storage und GPU
- 🖼 Verwaltung von Runtime-Image-Overrides ueber das Admin-Panel
- 🛰 Admin-Dashboard fuer Cluster-Ressourcenuebersicht und Instanzgesundheit
- 👥 CSV-basierter Massenimport von Benutzern und zentrale Quota-Zuweisung
- 🌐 Mehrsprachige UI sowie rollenbasierte Admin-/Benutzeransichten

## 🔄 Typical Workflow

1. 👨‍💼 Ein Administrator meldet sich an und konfiguriert Benutzer, Quotas und Runtime-Image-Einstellungen.
2. 🖥 Ein Benutzer erstellt eine Desktop-Instanz wie OpenClaw, Webtop oder Ubuntu.
3. ☸️ ClawManager erstellt die Kubernetes-Ressourcen und haelt den Runtime-Status synchron.
4. 🔐 Der Benutzer greift ueber das Portal oder den tokenbasierten Proxy-Endpunkt auf den Desktop zu.
5. 📊 Administratoren ueberwachen Instanzgesundheit und Cluster-Ressourcen ueber das Admin-Dashboard.

## 🏗 Architecture

```text
Browser
  -> ClawManager Frontend (React + Vite)
  -> ClawManager Backend (Go + Gin)
  -> MySQL
  -> Kubernetes API
  -> Pod / PVC / Service
  -> OpenClaw / Webtop / Linux Desktop Runtime
```

### High-Level Design

- Frontend: React 19 + TypeScript + Tailwind CSS
- Backend: Go + Gin + upper/db + MySQL
- Runtime: Kubernetes
- Zugriffsschicht: authentifizierter Reverse Proxy mit WebSocket-Weiterleitung
- Datenschicht: MySQL fuer Geschaeftsdaten, PVC fuer persistente Instanzdaten

## 🗂 Project Structure

```text
ClawManager/
├── backend/            # Go-Backend-API
├── frontend/           # React-Frontend
├── deployments/        # Container- und Kubernetes-Deployment-Dateien
├── dev_docs/           # Design- und Implementierungsdokumente
├── scripts/            # Hilfsskripte
├── TASK_BREAKDOWN.md   # Detaillierte Aufgabenaufschluesselung
└── dev_progress.md     # Entwicklungsfortschrittsprotokoll
```

## 💻 Tech Stack

### Backend

- Go 1.21+
- Gin
- upper/db
- MySQL 8.0+
- JWT-Authentifizierung

### Frontend

- React 19
- TypeScript 5.9
- Vite 7
- Tailwind CSS 4
- React Router

### Infrastructure

- Kubernetes
- Docker
- Nginx

## ☸️ Kubernetes Prerequisites

ClawManager ist ein Kubernetes-first-Projekt. Verwaltete Nodes muessen einem Kubernetes-Cluster beitreten, bevor ClawManager Instanzen planen, Ressourcen pruefen oder zentralisierte Operationen bereitstellen kann.

Vor der Installation von ClawManager sollte eine funktionierende Kubernetes-Umgebung vorhanden sein, und `kubectl` muss Zugriff auf den Cluster haben:

```bash
kubectl get nodes
```

### Linux-Setup-Beispiele

Mit `k3s`:

```bash
curl -sfL https://get.k3s.io | sh -
sudo kubectl get nodes
```

Mit `microk8s`:

```bash
sudo snap install microk8s --classic
sudo microk8s status --wait-ready
sudo microk8s kubectl get nodes
```

### Grundlegende Kubernetes-Befehle

```bash
kubectl get nodes
kubectl get pods -A
kubectl get pvc -A
kubectl cluster-info
```

### Mindestempfehlung

- 1 Kubernetes-Node
- 4 CPU
- 8 GB RAM
- 20+ GB freier Speicher

Wenn mehrere Desktop-Instanzen gleichzeitig laufen sollen, sollten mehr CPU, RAM und Storage eingeplant werden.

## 📦 Installation

Vor der Installation sicherstellen, dass:

- Kubernetes verfuegbar ist
- `kubectl get nodes` funktioniert

Direkt mit dem mitgelieferten Kubernetes-YAML deployen:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
kubectl get pods -A
kubectl get svc -A
```

## ⚡ Quick Start

### Default Accounts

- Standard-Admin-Konto: `admin / admin123`
- Standardpasswort fuer importierte Admin-Benutzer: `admin123`
- Standardpasswort fuer importierte regulaere Benutzer: `user123`

### First Login

1. 👨‍💼 Als Administrator anmelden.
2. 👥 Benutzer erstellen oder importieren und Quotas zuweisen.
3. 🧩 Optional Runtime-Image-Karten in den Systemeinstellungen konfigurieren.
4. 🖥 Als Benutzer anmelden und eine Instanz erstellen.
5. 🔗 Ueber Portal View oder Desktop Access auf den Desktop zugreifen.

## ⚙️ Configuration

ClawManager folgt einem klaren Sicherheitsmodell:

- Instanz-Services verwenden das interne Kubernetes-Netzwerk
- Desktop-Zugriff laeuft ueber den authentifizierten ClawManager-Backend-Proxy
- backend sollte idealerweise im Cluster betrieben werden
- Runtime-Images koennen zentral ueber die Systemeinstellungen verwaltet werden
- alle verwalteten Nodes sollten zum selben Kubernetes-Cluster gehoeren

Wichtige Backend-Umgebungsvariablen:

- `SERVER_ADDRESS`
- `SERVER_MODE`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `JWT_SECRET`

Im Frontend-Entwicklungsmodus wird `/api` ueber Vite an das Backend weitergeleitet.

### CSV Import Template

```csv
Username,Email,Role,Max Instances,Max CPU Cores,Max Memory (GB),Max Storage (GB),Max GPU Count (optional)
```

Hinweise:

- `Email` ist optional
- `Max GPU Count (optional)` ist optional
- alle anderen Spalten sind erforderlich
- Quota-Werte sollten zur Kapazitaetsplanung des Clusters passen

## 📄 License

Dieses Projekt ist unter der MIT License veroeffentlicht.

## ❤️ Open Source

Issues und Pull Requests sind willkommen, einschliesslich Verbesserungen an Funktionen, Dokumentation und Tests.
