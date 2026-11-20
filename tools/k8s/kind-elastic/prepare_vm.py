#!/usr/bin/env python3
"""Allow bubblewrap user namespaces only in the dedicated local Colima VM."""
from pathlib import Path
import subprocess

profile = Path(__file__).with_name("bwrap.apparmor").read_text()
script = f"""cat > /etc/apparmor.d/cm-kind-bwrap <<'EOF'
{profile.rstrip()}
EOF
apparmor_parser -r /etc/apparmor.d/cm-kind-bwrap
"""
subprocess.run(['colima', '--profile', 'cm-kind', 'ssh', '--', 'sudo', 'sh', '-eu'],
               input=script, text=True, check=True, cwd='/tmp')
print('Loaded bubblewrap profile inside Colima cm-kind')
