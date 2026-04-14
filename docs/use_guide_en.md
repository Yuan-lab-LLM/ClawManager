[<- Back to README](../README.md)

# ClawManager Deployment and Quick Start Guide

## Table of Contents
- [I. Environment and Goals](#sec-01)
- [II. Deployment Options Overview](#sec-02)
- [III. Option A: Deploy with k3s](#sec-03)
- [IV. Option B: Deploy with Standard Kubernetes](#sec-04)
- [V. Recommendations for Image Pulling on Mainland China Networks (Optional)](#sec-05)
- [VI. Deploy ClawManager](#sec-06)
- [VII. Launch the Web Page](#sec-08)
- [VIII. Quick Start Guide (Initialize and Create an OpenClaw Instance After Login)](#sec-09)
- [IX. Console and Other AI Gateway Features](#sec-12)
- [X. Workspace Module Guide](#sec-13)
- [XI. Quick Troubleshooting Reference](#sec-14)
- [XII. Recommended Final Check Sequence (Use as a Self-Check)](#sec-15)

<a id="sec-01"></a>
## I. Environment and Goals
- **System assumption**: `x86_64` Linux server.
- **Deployment goal**: Deploy **ClawManager**, complete secure model configuration in the Web UI, and then create and start an **OpenClaw Desktop** instance.
- **Applicable scenarios**:
  - **Option A: k3s single-node/lightweight cluster deployment**
  - **Option B: standard Kubernetes cluster deployment** (such as kubeadm clusters, enterprise Kubernetes clusters, and cloud-hosted Kubernetes clusters)


---

<a id="sec-02"></a>
## II. Deployment Options Overview
You can deploy using either of the following methods:

### Option A: k3s deployment
Suitable for single-node, test, or lightweight production environments.

### Option B: standard Kubernetes deployment
Suitable for server environments that already have a standard Kubernetes cluster.

No matter which method you use, you will ultimately apply the same ClawManager manifest:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
```

---

<a id="sec-03"></a>
## III. Option A: Deploy with k3s

### 3.1 Install k3s
```bash
curl -sfL https://get.k3s.io | sh -
```

For mainland China networks, you can install using a mirror source:

```bash
curl -sfL https://rancher-mirror.rancher.cn/k3s/k3s-install.sh |   INSTALL_K3S_MIRROR=cn sh -
```

### 3.2 Check service status
```bash
sudo systemctl status k3s --no-pager
sudo systemctl enable k3s
```

### 3.3 Configure kubectl
If the current user cannot use `kubectl` directly, run:

```bash
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown "$USER:$USER" ~/.kube/config
```

Or set it temporarily:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

### 3.4 Verify the cluster
```bash
kubectl get nodes
```

Normally, you should see the node in the `Ready` state.

---

<a id="sec-04"></a>
## IV. Option B: Deploy with Standard Kubernetes

> Applies to x86 server environments that already have an available Kubernetes cluster.

### 4.1 Prerequisite checks
Confirm that the current `kubectl` is connected to the target cluster:

```bash
kubectl get nodes
kubectl get ns
```

Normally, you should see at least one `Ready` node.

### 4.2 Check the default StorageClass
MySQL and MinIO in ClawManager require persistent storage. It is recommended to first check whether the cluster has a default `StorageClass`:

```bash
kubectl get storageclass
```

If the cluster already has a default storage class, you can continue with deployment directly.

If there is **no default StorageClass**, it is recommended to prepare available PV / PVC resources or use a local path storage solution in advance; otherwise, you may later encounter:

```text
pod has unbound immediate PersistentVolumeClaims
```

---

<a id="sec-05"></a>
## V. Recommendations for Image Pulling on Mainland China Networks (Optional)
If the server accesses Docker Hub or other public registries slowly, you can configure image acceleration.

### 5.1 k3s scenario: configure `/etc/rancher/k3s/registries.yaml`
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

After modifying it, run:

```bash
sudo systemctl restart k3s
```

### 5.2 Verify image pulling
```bash
sudo k3s crictl pull docker.io/rancher/mirrored-pause:3.6
```

---

<a id="sec-06"></a>
## VI. Deploy ClawManager

### 6.1 Pull the project code
```bash
git clone https://github.com/Yuan-lab-LLM/ClawManager.git
cd ClawManager
```

### 6.2 Apply the deployment manifest
Run in the repository root directory:

```bash
kubectl apply -f deployments/k8s/clawmanager.yaml
```

### 6.3 Check base resources
```bash
kubectl get ns
kubectl get pods -n clawmanager-system
kubectl get svc -n clawmanager-system
```

Under normal circumstances, you will see the following components:
- `clawmanager-app`
- `mysql`
- `minio`
- `skill-scanner`

If you see the following error:

```text
0/1 nodes are available: pod has unbound immediate PersistentVolumeClaims
```

it means MySQL / MinIO in cluster storage cannot start because the PVC is not bound. Please jump directly to the end of this document:

- [XI.1 Dedicated Handling for Storage Issues (PV/PVC)](#sec-14-storage)

---

<a id="sec-08"></a>
## VII. Launch the Web Page

### 7.1 Access via NodePort
By default, the ClawManager frontend Service uses an HTTPS NodePort. You can check it first:

```bash
kubectl get svc -n clawmanager-system
```

If the frontend port is:

```text
443:30443/TCP
```

you can access it directly in the browser:

```text
https://<serverIP>:30443
```


### 7.2 First HTTPS access note
Since it usually uses a self-signed certificate, the browser may show an “unsafe” or certificate warning. Click:

```text
Advanced → Continue to visit
```

to enter the page.

---

<a id="sec-09"></a>
## VIII. Quick Start Guide (Initialize and Create an OpenClaw Instance After Login)

After completing the deployment above and successfully opening the management page, you still need to finish the following initialization steps before you can actually create and start an **OpenClaw** instance.

### 8.1 Log in to the system
1. Open the deployed page, for example: `https://<nodeIP>:30443`.
2. Log in with the default administrator account:
   - **Username**: `admin`
   - **Password**: `admin123`
3. After first login, it is recommended to change the default password as needed.


### 8.2 Configure the secure model (AI Gateway)

![Figure 1: AI Gateway configuration](./main/1.png)
After logging in, you need to configure an available **secure model** first so that it can be used uniformly by the platform and subsequent instances.

1. Click the left-side menu: **AI Gateway** → **Models**.
2. Add a new model or edit an existing one, and fill in the following information according to the actual model service you connect:

   * **Display Name**: Enter a name that is easy to identify.
   * **Vendor Template**: Choose the corresponding template based on your model service type; if you use a custom or compatible interface, you can select **Local / Internal**.
   * **Protocol**: Select the protocol according to the interface, such as **OpenAI Compatible** or another actual protocol.
   * **Base URL**: Enter the endpoint address provided by the model service.
   * **API Key**: Enter the valid key for the corresponding model service.
   * **Provider Model**: Enter the actual model name to call.
   * **Currency**: Fill it in according to your situation; if no billing display is needed, you can keep the default.
   * **Input Price / Output Price**: If billing statistics are not needed, you can first fill in `0`.
3. Be sure to check the following before submission:

   * **Secure Model**
   * **Enabled**
4. Click **Save**.

> Note: The images on the page are only used to show the field positions and example format. The actual content should be based on the model service configuration you use.


### 8.3 Create an OpenClaw instance
After the model configuration is completed, create an **OpenClaw Desktop** instance.

1. Click **ADMIN** in the lower-left corner and switch to the **Workspace**.
2. Click **Create Instance**.

![](./main/2.png)
#### Step 1: Basic Information
- Fill in the **Instance Name** (at least 3 characters).
- The description is optional and may be left blank.
- Click **Next**.

![](./main/3.png)
#### Step 2: Select Type
- Select **OpenClaw Desktop**.
- Click **Next**.


![](./main/4.png)
#### Step 3: Configuration
- You can directly choose the **Small** specification:
  - `2 CPU`
  - `4 GB RAM`
  - `20 GB Disk`
- You can also modify the settings as needed in the custom configuration section below.
- For the OpenClaw resource injection section, you can choose as needed:
  - **Manual Resources**
  - **Resource Bundle**
  - **Archive Import**
- For first-time use, you can keep the default or select **Manual Resources**.
- Finally, click **Create**.

### 8.4 First creation note
- When creating an **OpenClaw** instance for the first time, the required images must be downloaded and the environment must be initialized, so it will take noticeably longer.
- On slow networks or during the first image pull, the instance status may remain at **Creating** for a long time. Please wait patiently.
- If it still does not start successfully after a long time, go back to the Kubernetes / Docker logs to troubleshoot image, PVC, gateway model, and other issues.

---

<a id="sec-12"></a>
## IX. Console and Other AI Gateway Features

In addition to model configuration, the platform homepage console and the AI Gateway also provide auditing, cost, and rule governance features, making it easier for administrators to centrally view cluster status, model call records, and security policy execution status.

### 9.1 Console Overview

![](./main/5.png)

The console homepage is used to display the overall running status of the current cluster and platform, allowing administrators to quickly understand resource usage and system health.

It mainly includes the following information:

- **Cluster Basic Information Overview**: Displays the total number of users, total number of instances, number of running instances, and total storage usage of the current platform.
- **Node Overview**: Displays the current number of available nodes, as well as the main scheduling node information in the current cluster.
- **Resource Request Status**: Displays the total amount of CPU, memory, and disk resources that have been requested by the current platform.
- **Capacity Dashboard**: Shows overall resource capacity and current usage rates by node, CPU, memory, disk, and other dimensions, making it easier to determine whether the cluster still has available capacity.
- **Infrastructure Table**: Used to view the status information of current nodes, resources, and the basic runtime environment.

> Note: The console is mainly used to view the overall platform resources, nodes, and instance operation summary, and is not used directly for specific OpenClaw operations inside an instance.

### 9.2 Security Center (skill-scanner)

The **Security Center** in the console is used to centrally view the scanning status of platform resources, historical reports, and scanner configurations. It relies on the backend **skill-scanner** service and can be used to perform static scanning, deep scanning, and supplementary LLM-based analysis on resources, thereby helping administrators identify potential risky content, abnormal resources, and suspicious skills.

The Security Center currently includes the following three modules:

* **Runtime Overview**
* **Report History**
* **Scanner Configuration**

#### 9.2.1 Runtime Overview

![](./main/14.png)

The “Runtime Overview” page is used to view the overall scanning status and risk distribution of the current platform, helping administrators quickly understand the current security posture.

The page mainly includes the following information:

* **Current Active Mode**: Displays whether the system is currently using **Quick Mode** or **Deep Mode**.
* **Quick Scan / Full Scan**:

  * **Quick Scan**: Suitable for handling newly added or modified resources, with a lighter scan scope and faster execution speed.
  * **Full Scan**: Suitable for periodically rescanning all resources to fully review the current state of all platform resources.
* **Total Assets**: The number of resources currently included in the scanning scope of the Security Center.
* **Completed Scans**: The number of resources that have completed scanning.
* **High Risk / Medium Risk**: Statistics on the risk levels identified in the current scanning results.
* **Scan Coverage**: Shows the proportion of assets that have actually completed scanning relative to the total assets on the platform.
* **SAFE / High Risk / Pending / Failed**:

  * **SAFE**: Number of resources that passed the scan and currently have no detected risks
  * **High Risk**: Number of risky assets that require immediate handling
  * **Pending**: Number of resources waiting for evidence collection or queued for scanning
  * **Failed**: Number of scan tasks that failed and need to be rerun
* **Platform Asset Risk Trend**: Displays the current risk distribution of platform assets aggregated by risk level.
* **Hot Assets**: Displays the most frequently used skills or high-frequency resources to help administrators quickly locate key assets.
* **Scanner Status**: Displays the availability and connection status of the current skill-scanner, such as “Static scanning available” and “Connected”.
* **Risk Alerts and Handling Suggestions**: Provides brief alert information based on the current risk posture.
* **Recent Scan Tasks**: Displays recently executed scan records for easier review of recent scanning activities.

> Notes:
>
> * When the page shows “There are currently no high-risk or medium-risk assets,” it means no significant risks have been found in the current scan results.
> * When the page shows “There are no scan task records yet,” it means no scans have been executed yet, or no valid scan results have been generated.

#### 9.2.2 Report History

The “Report History” page is used to view historical scan reports and related result records, making it easier for administrators to review past scan executions.

This module is mainly used for:

* Viewing the results of previously executed scan tasks
* Comparing scan outputs at different points in time
* Assisting in tracking security changes of a specific resource at different stages
* Providing historical references for subsequent review, rescanning, and issue troubleshooting

> Notes:
>
> * “Report History” focuses more on archiving and reviewing historical results;
> * “Runtime Overview” focuses more on current status and overall overview.

#### 9.2.3 Scanner Configuration

![](./main/15.png)

The “Scanner Configuration” page is used to manage the operating mode of skill-scanner, LLM-related settings, and the two scanning strategies: quick and deep. After saving, a Deployment rollout will be triggered, and the system will wait for the new configuration to take effect.

The page mainly includes the following content:

##### (1) skill-scanner Service Status

* Displays the namespace, Deployment name, and connection status of the current backend scanning service.
* When the page shows **Connected** and **Static scanning available**, it means the basic static scanning capability is available.

##### (2) LLM Configuration

This section is used to configure the primary LLM so that the scanner can perform model-based analysis when needed.

The main fields include:

* **Primary LLM Integration**: The primary LLM configuration can be imported directly from a model already configured in **AI Gateway**.
* **LLM API Key**: Corresponds to `SKILL_SCANNER_LLM_API_KEY`, used for authentication of the primary LLM analyzer.
* **LLM Model**: Corresponds to `SKILL_SCANNER_LLM_MODEL`, for example a specific model name.
* **LLM Base URL**: Corresponds to `SKILL_SCANNER_LLM_BASE_URL`, used to configure the primary LLM service endpoint.

##### (3) Meta LLM Integration

This section is used to configure the model used by the meta analyzer, typically for further summarization, aggregation, or secondary processing of findings.

The main fields include:

* **Meta LLM Integration**: The meta analyzer configuration can be imported directly from a model already configured in **AI Gateway**.
* **Meta LLM API Key**: Corresponds to `SKILL_SCANNER_META_LLM_API_KEY`.
* **Meta LLM Model**: Corresponds to `SKILL_SCANNER_META_LLM_MODEL`.
* **Meta LLM Base URL**: Corresponds to `SKILL_SCANNER_META_LLM_BASE_URL`.

> Notes:
>
> * If no LLM is currently configured, the page will usually indicate that only static scanning is supported at the moment;
> * Only after configuring both the primary LLM and the Meta LLM can the scanner enable more complete semantic analysis and summarization capabilities.

##### (4) Current Scanning Mode

The page supports selecting the scanning mode currently used by the platform:

* **Quick Mode**: Uses quick analyzers for scanning and is suitable for daily rapid checks.
* **Deep Mode**: Uses deep analyzers for scanning and is suitable for more complete and in-depth analysis.

It should be noted that:

* Both “Quick Scan” and “Full Scan” on the Dashboard will use the scan strength selected here;
* Their main difference lies in the scan scope, not in the analyzer depth itself.

##### (5) Quick / Deep Scanning Strategy

The lower part of the page maintains two sets of scanning strategy configurations, **Quick** and **Deep**, so that administrators can choose different analyzer combinations for different scenarios.

Each strategy includes the following configuration items:

* **Timeout (seconds)**: Sets the timeout for scan tasks under the current mode.
* **Invocation Methods**: Different analyzers can be enabled or disabled as needed.

The currently visible analyzer types include:

* **Static**: YAML + YARA static rule scanning
* **Bytecode**: Python bytecode integrity verification
* **Pipeline**: Command chain and taint analysis
* **Behavioral**: AST-based behavior and data flow analysis
* **LLM**: Semantic analysis relying on external LLMs
* **Meta**: Secondary summarization analysis of findings

These can usually be understood as follows:

* **Quick Mode**: Focuses on faster execution and is often used for daily incremental checks
* **Deep Mode**: Can enable more analyzers and is suitable for deeper review and security auditing

##### (6) Save and Apply

The **Save and Apply** button in the upper-right corner is used to submit all current scanner-related configurations. After saving, it will:

* Update the quick / deep scanning strategies in ClawManager
* Update the related environment variables of the skill-scanner Deployment
* Wait for the rollout to complete before the new configuration officially takes effect

> Notes:
>
> * After modifying scanner configurations, it is recommended to wait until the configuration has fully taken effect before executing new scan tasks;
> * If the connection status becomes abnormal after configuration changes, it is recommended to first check the AI Gateway model, LLM endpoint, Key, and Deployment rollout status.

### 9.3 AI Gateway Feature Overview

In addition to model configuration, AI Gateway also includes the following modules:

* **AI Audit**: View model invocation traces, request and response payloads, hit risks, routing decisions, and invocation details.
* **Cost**: View token usage, estimated cost, internal cost, and trend statistics.
* **Risk Control Rules**: Configure sensitive detection rules to control whether matched content is allowed through or routed to the security model.

### 9.4 Cost Module

The Cost page is used to count the cost and token usage of platform model calls, helping administrators understand overall consumption.

![](./main/6.png)

The page mainly includes the following content:

* **Input Tokens**: Statistics of the total input prompt tokens.
* **Output Tokens**: Statistics of the total tokens generated by the model.
* **Estimated Cost**: Cost estimated according to the Provider's unit price.
* **Internal Cost**: Internal accounting cost related to the security model.
* **Daily Cost Trend**: View estimated cost and token changes within the current window over the last 7 days.
* **User Summary**: Aggregated usage and cost by user.
* **Instance Summary**: Aggregated usage and cost by instance.
* **Recent Cost Records**: Supports searching and paginated viewing of cost records by Trace, user, model, and other conditions, and can further jump to audit details.

> Note: If no model invocation records have been generated yet, input tokens, output tokens, cost, and trend charts may all be 0, which is normal.

### 9.5 AI Audit Module

The AI Audit page is used to view recent managed model invocation records, helping administrators troubleshoot model invocations, token usage, and routing results.

![](./main/7.png)

The main functions include:

* **Recent AI Trace**: View recent model invocation chains.
* **Trace List**: View recent managed traces in a unified table.
* **Search and Filtering**: Supports searching by Trace, request content, user, model, and other conditions.
* **Status Filtering**: Supports viewing different invocation results by status.
* **Model Filtering**: Supports filtering corresponding invocation records by model.
* **Pagination and Refresh**: Supports paginated viewing and manual refresh of the latest audit results.

> Note: If the page shows “No AI audit records yet,” it means that no actual model invocation requests have been generated yet.

### 9.6 Risk Control Rules Module

The Risk Control Rules page is used to configure sensitive content detection rules and determine the action to be taken after a rule is hit.

![](./main/8.png)

This module mainly supports:

* **Rule List Management**: View all rules and their enabled status.
* **Rule Category View**: Supports viewing rules by categories such as personal information, company information, customer business, security credentials, finance and legal, politically sensitive, and custom.
* **Rule Field Configuration**: Supports setting rule ID, display name, severity level, action, order, regex pattern, and description.
* **Rule Action Control**: When a rule is hit, it can be configured to allow the content or route it to the security model.
* **Batch Enable / Disable**: Supports batch adjustment of rule status.
* **Rule Test Console**: Paste sample text to test which enabled rules or draft rules will be triggered.

The built-in rule examples currently include, but are not limited to:

* Personal information: email address, mobile number, ID card number, passport number, bank card context, address, resume content, etc.
* Company information: internal IP, internal domain name, host naming, Kubernetes Service DNS, project code name, organizational structure, salary / HR information, etc.
* Customer business: customer list, contracts / quotations, invoice tax IDs, CRM / ticket data, etc.
* Security credentials: private keys, API keys, tokens, JWT, Cookie / Session, database connection strings, kubeconfig, environment variable secrets, etc.
* Finance and legal: budget, profit, revenue, legal opinions, litigation, NDA, etc.
* Politically sensitive: political institutions, military/national security, extremist and violent expressions, etc.

> Note: Default rules already cover many common sensitive information detection scenarios. In actual use, rules can be further added, adjusted, or disabled according to business requirements.
---

<a id="sec-13"></a>
## X. Workspace Module Guide

The Workspace is the main operating area after a regular user enters the platform. It is used to view personal resource quotas, create instances, manage instances, and maintain OpenClaw-related resources. This module is more oriented toward daily use and operations than the administrator-side “Console Overview”.

### 10.1 Workspace Home
![](./main/9.png)
The Workspace home page is used to display the instance and resource usage summary of the current account, and mainly includes the following contents:

- **My Instances**: Displays the number of instances created under the current account.
- **Running**: Displays the number of instances currently running.
- **Used Storage**: Displays the amount of storage space currently occupied by the account.
- **My Resource Quotas**: Shows the available quota information of the current account, including the number of instances, maximum CPU cores, maximum memory, maximum storage, and maximum GPU count.
- **Quick Actions**: Provides two entry points: **Create New Instance** and **View All Instances**, so you can get started quickly with the platform.

> Note: When the page shows “No instances yet”, you can directly click **Create New Instance** to start creating the first OpenClaw Desktop instance.

### 10.2 My Instances

The **My Instances** page is used to centrally view and manage all instances created under the current account. This page mainly carries the instance management functions.
![](./main/10.png)
Common supported operations include:

- **View instance status**: Check whether the instance is being created, running, stopped, or in an abnormal state.
- **Open instance details**: View basic instance information, resource configuration, and runtime status.
- **Stop instance**: When the instance is abnormal or the environment needs to be reloaded, you can perform a stop operation.
- **Delete instance**: When the instance is no longer needed, you can delete it directly to release the corresponding CPU, memory, and storage resources.

> Note: After deleting an instance, the related resources of the instance will be cleaned up together. Before executing, make sure that the data and configuration inside it have been backed up.

### 10.3 Resource Management

The **Resource Management** page is used to maintain the OpenClaw resource content available for use, making it easy to inject and use after an instance starts.
![](./main/11.png)
The page mainly includes the following parts:

- **Resources**: View and maintain available resource entries.
- **Resource Bundles**: Combine multiple resources into reusable bundles to facilitate batch injection.
- **Injection Records**: View resource injection history and execution status.

On the left side of the Resource Management page, you can also manage resources by type. The currently visible types on the page include:

- **Channels**
- **Skills**
- **Agents (coming soon)**
- **Scheduled Tasks (coming soon)**

The upper-right corner of the page supports:

- **Refresh**: Reload the current resource list.
- **New**: Create a new resource item.

> Note: Resource Management is mainly used to prepare OpenClaw resource content that can be used after the instance starts, and does not directly replace the instance creation process. When creating an instance, resources can be injected through methods such as **Manual Resources**, **Resource Bundles**, and **Archive Import**.


### 10.3.1 Create a Channel

A "Channel" is used to configure how OpenClaw connects to external messaging platforms or access endpoints, such as Telegram, Slack, and Feishu / Lark.

![](./main/12.png)

To create a channel, follow these steps:

1. Go to the **Resource Management** page and stay on the **Resources** tab.
2. In the resource type list on the left, select **Channel**.
3. Click **New** on the right side of the page to open the "Create Resource" dialog.
4. Fill in the basic information in the dialog:
   - **Type**: select **Channel**
   - **Resource Key**: enter the unique identifier for this channel. It is recommended to use an easy-to-recognize and non-duplicated English name or combined identifier
   - **Name**: enter the display name of the channel
   - **Tags**: optional, used for classification and search
   - **Description**: optional, used to supplement the purpose of the channel
   - **Enabled**: it is recommended to keep this checked
5. In the **Channel Template** section, choose an initial template. The currently supported templates include:
   - `Telegram`
   - `Slack`
   - `Feishu / Lark`

6. After selecting a template, click **Load Template**. The system will automatically write the basic configuration of the corresponding template into the **Content JSON** section below.
7. Based on your actual integration information, continue to supplement or modify the fields in **Content JSON**.
8. After confirming the configuration is correct, click Save to complete channel creation.

> Notes:
> - **Channel Template** helps you quickly generate a basic configuration;
> - **Content JSON** is the final effective channel configuration content;
> - If there is no fully matching template, you can also manually fill in the configuration directly in **Content JSON**.

### 10.3.2 Upload Skills

Skills are used to provide reusable functional capabilities for OpenClaw. The platform supports batch importing skills by uploading archive files.

![](./main/13.png)

To upload skills, follow these steps:

1. Go to the **Resource Management** page and stay on the **Resources** tab.
2. In the resource type list on the left, select **Skills**.
3. Click **Choose File** and select a local skill archive.
4. The current page only supports uploading **`.zip`** files.
5. After selecting the file, click **Upload Skill Archive** on the right.
6. The system will automatically parse the uploaded content and import each first-level directory as one skill.
7. After the upload is complete, you can view the imported skills in the skill list.

> Notes:
> - It is recommended to organize the skill archive in advance by directory;
> - Each first-level directory will be recognized as an independent skill;
> - If the list is not refreshed immediately after upload, you can manually click **Refresh** in the upper-right corner of the page to reload it.
---

<a id="sec-14"></a>
## XI. Quick Troubleshooting Reference

<a id="sec-14-storage"></a>
### 11.1 Dedicated Handling for Storage Issues (PV/PVC)

If you see the following error:

```text
0/1 nodes are available: pod has unbound immediate PersistentVolumeClaims
```

it means the cluster storage was not bound automatically. In this case, you can manually create local `hostPath` PV/PVC in the x86 single-node server style.

> This solution is suitable for single-node server testing or lightweight environments. For production environments, it is recommended to use formal storage such as NFS, Ceph, or cloud disks instead.

#### 11.1.1 Create PV
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

#### 11.1.2 Create PVC
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

#### 11.1.3 Recreate Pod
```bash
kubectl delete pod --all -n clawmanager-system
```

#### 11.1.4 Observe status again
```bash
kubectl get pvc -n clawmanager-system
kubectl get pods -n clawmanager-system -w
```

Expected results:
- `mysql-data` / `minio-data` are `Bound`
- `mysql` / `minio` / `skill-scanner` / `clawmanager-app` are finally `Running`

---

| Symptom | Cause | Fix |
| :--- | :--- | :--- |
| `kubectl` connection to `localhost:8080` is refused | kubeconfig is not configured | Set `KUBECONFIG` or copy it to `~/.kube/config` |
| Pod image pull timeout | Network to Docker Hub / GHCR is unstable | Configure image acceleration or a proxy |
| MySQL / MinIO remain `Pending` | PVC not bound | Check the `StorageClass` or manually create PV/PVC |
| The browser cannot open the page | NodePort is not open / the `port-forward` process was not kept running | Open the port or keep the forwarding terminal running |
| The page opens but an OpenClaw instance cannot be created | Secure model is not configured | First configure and enable the secure model under **AI Gateway → Models** |
| The instance remains “Creating” for a long time | The first image pull takes a long time / storage or network issues | Wait patiently, and if necessary check Pods and events |

---

<a id="sec-15"></a>
## XII. Recommended Final Check Sequence (Use as a Self-Check)
1. `kubectl get nodes`
2. `kubectl get storageclass`
3. `kubectl get pods -n clawmanager-system`
4. `kubectl get pvc -n clawmanager-system`
5. `kubectl get svc -n clawmanager-system`
6. Open `https://<IP>:30443` in a browser
7. Log in to the backend and complete **secure model configuration**
8. Create an **OpenClaw Desktop** instance in the Workspace
