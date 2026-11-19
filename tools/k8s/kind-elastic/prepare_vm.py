#!/usr/bin/env python3
"""Allow bubblewrap user namespaces only in the dedicated local Colima VM."""
import subprocess

script = """cat > /etc/apparmor.d/cm-kind-bwrap <<'EOF'
abi <abi/4.0>,
include <tunables/global>
profile cm-kind-bwrap /usr/bin/bwrap flags=(unconfined) {
  userns,
}
EOF
apparmor_parser -r /etc/apparmor.d/cm-kind-bwrap
"""
subprocess.run(['colima', '--profile', 'cm-kind', 'ssh', '--', 'sudo', 'sh', '-eu'],
               input=script, text=True, check=True, cwd='/tmp')
print('Loaded bubblewrap profile inside Colima cm-kind')
