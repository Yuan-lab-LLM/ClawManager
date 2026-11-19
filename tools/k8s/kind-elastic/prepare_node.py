#!/usr/bin/env python3
"""Prepare only the named kind node for nested user namespaces and local PVCs."""
import argparse, json, subprocess

p = argparse.ArgumentParser()
p.add_argument('--docker-context', default='colima-cm-kind')
p.add_argument('--node', default='cm-elastic-control-plane')
p.add_argument('--cluster', default='cm-elastic')
p.add_argument('--kubeconfig', required=True)
p.add_argument('--kubectl', default='kubectl')
p.add_argument('--proxy', help='Optional proxy address reachable from the VM, never host loopback')
a = p.parse_args()
docker = ['docker', '--context', a.docker_context, 'exec']
identity = json.loads(subprocess.check_output(['docker', '--context', a.docker_context, 'inspect', a.node]))[0]
if identity['Config'].get('Labels', {}).get('io.x-k8s.kind.cluster') != a.cluster:
    raise SystemExit('Refusing to modify a container outside the expected kind cluster')
kube = [a.kubectl, '--kubeconfig', a.kubeconfig]
node = json.loads(subprocess.check_output(kube + ['get', 'node', a.node, '-o', 'json']))
if not node['spec'].get('providerID', '').startswith('kind://docker/' + a.cluster + '/'):
    raise SystemExit('Kubeconfig does not identify the expected kind node')

def shell(script):
    subprocess.run(docker + ['-i', a.node, 'sh', '-eu'], input=script, text=True, check=True)

# Only trusted kind-node mounts are changed. Business containers retain their
# private namespaces, restricted capabilities and hidden sysfs/management paths.
shell('''
chmod 755 /kind /kind/bin
mkdir -p /etc/containerd/certs.d/172.16.1.12:5010
cat > /etc/containerd/certs.d/172.16.1.12:5010/hosts.toml <<'EOF'
server = "http://172.16.1.12:5010"
[host."http://172.16.1.12:5010"]
  capabilities = ["pull", "resolve"]
EOF
mkdir -p /unmasked-sys /data/clawmanager/system/workspaces
cat > /etc/systemd/system/cm-kind-sysfs.service <<'EOF'
[Unit]
Description=Visible read-only sysfs for nested kind user namespaces
Before=kubelet.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'mountpoint -q /unmasked-sys || mount -t sysfs -o ro,nosuid,nodev,noexec sysfs /unmasked-sys'
[Install]
WantedBy=multi-user.target
EOF
if ! mountpoint -q /data/clawmanager/system/workspaces; then
  if [ -n "$(ls -A /data/clawmanager/system/workspaces)" ]; then
    echo 'Refusing to hide existing workspace data; prepare before deployment' >&2
    exit 1
  fi
  if [ ! -f /var/local/cm-workspaces.ext4 ]; then
    truncate -s 8G /var/local/cm-workspaces.ext4
    mkfs.ext4 -q /var/local/cm-workspaces.ext4
  fi
fi
cat > /etc/systemd/system/cm-kind-workspaces.service <<'EOF'
[Unit]
Description=ID-mappable ext4 backing for local kind workspace PVC
Before=kubelet.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'mountpoint -q /data/clawmanager/system/workspaces || mount -o loop,nosuid,nodev /var/local/cm-workspaces.ext4 /data/clawmanager/system/workspaces'
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now cm-kind-sysfs.service cm-kind-workspaces.service
''')
if a.proxy:
    # JSON is systemd's quoted Environment= syntax, not shell interpolation.
    env = ' '.join(json.dumps(k + '=' + v) for k, v in {
        'HTTP_PROXY': a.proxy, 'HTTPS_PROXY': a.proxy,
        'NO_PROXY': '127.0.0.1,localhost,172.16.1.12,172.18.0.0/16,10.244.0.0/16,10.96.0.0/12,.svc,.cluster.local',
    }.items())
    for service in ['containerd', 'kubelet']:
        path = '/etc/systemd/system/' + service + '.service.d'
        shell('mkdir -p ' + path + "\ncat > " + path + "/cm-proxy.conf <<'EOF'\n[Service]\nEnvironment=" + env + '\nEOF\n')
    shell('systemctl daemon-reload\nsystemctl restart containerd kubelet\n')
subprocess.run(kube + ['label', 'node', a.node, 'clawmanager.io/storage-node=true', '--overwrite'], check=True)
print('Prepared ' + a.node + '; runtime Pods remain unprivileged')
