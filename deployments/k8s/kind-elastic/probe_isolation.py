"""Read-only supervisor probe; report booleans, never credentials or config data."""
import json, os, pathlib, sys

identity, workspace, other = int(sys.argv[1]), sys.argv[2], sys.argv[3]
uid = 200000 + identity
protected = [other, '/run/clawmanager-slots', '/workspaces/.clawmanager-slot-locks',
             '/workspaces/.clawmanager-upgrades', '/var/lib/clawmanager-agent',
             '/var/lib/clawmanager-slot', '/var/run/clawmanager-agent-tls',
             '/var/run/secrets/kubernetes.io/serviceaccount', '/sys']
management = {'RUNTIME_AGENT_CONTROL_TOKEN', 'RUNTIME_AGENT_REPORT_TOKEN',
              'CLAWMANAGER_AGENT_CONTROL_TOKEN', 'CLAWMANAGER_AGENT_REPORT_TOKEN',
              'KUBERNETES_SERVICE_HOST'}
assert pathlib.Path('/proc/1/comm').read_text().strip() == 'tini'
rows = []
for directory in pathlib.Path('/proc').iterdir():
    if not directory.name.isdigit():
        continue
    try:
        status = dict(line.split(':', 1) for line in (directory/'status').read_text().splitlines() if ':' in line)
        if int(status['Uid'].split()[0]) != uid:
            continue
        nspids = status.get('NSpid', '').split()
        if len(nspids) < 2 or int(nspids[-1]) < 2:
            continue
        root = directory/'root'
        caps = all(int(status[field].strip(), 16) == 0 for field in ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb'))
        namespaces = {name: os.readlink(directory/'ns'/name) != os.readlink(pathlib.Path('/proc/1/ns')/name) for name in ('pid', 'user', 'mnt')}
        hidden = {path: not (root/path.lstrip('/')).exists() for path in protected}
        keys = {item.split(b'=', 1)[0].decode(errors='replace') for item in (directory/'environ').read_bytes().split(b'\0') if item}
        row = {'uid': uid, 'command': status['Name'].strip(), 'capabilities_zero': caps,
               'no_new_privs': status['NoNewPrivs'].strip() == '1',
               'seccomp_enforced': status['Seccomp'].strip() == '2', 'private_namespaces': namespaces,
               'protected_paths_hidden': hidden, 'management_environment_absent': not bool(keys & management),
               'own_workspace_visible': (root/workspace.lstrip('/')).is_dir()}
        row['passed'] = caps and row['no_new_privs'] and row['seccomp_enforced'] and all(namespaces.values()) and all(hidden.values()) and row['management_environment_absent'] and row['own_workspace_visible']
        rows.append(row)
    except (FileNotFoundError, ProcessLookupError):
        continue
result = {'instance_id': identity, 'passed': bool(rows) and all(row['passed'] for row in rows), 'processes': rows}
print(json.dumps(result))
assert result['passed'], 'Business process isolation failed'
