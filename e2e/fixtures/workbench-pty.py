"""Disposable browser-test PTY; never a production execution path."""
import os, sys, pty, fcntl, termios, struct, select, json, base64, signal
master, slave = pty.openpty()
pid = os.fork()
if pid == 0:
    os.close(master)
    os.setsid()
    fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
    for fd in (0, 1, 2):
        os.dup2(slave, fd)
    if slave > 2:
        os.close(slave)
    os.execve('/bin/sh', ['sh', '-i'], {'PATH': '/usr/bin:/bin', 'HOME': os.getcwd(), 'TERM': 'xterm-256color', 'PS1': 'test$ '})
os.close(slave)
try:
    while True:
        readable, _, _ = select.select([master, sys.stdin], [], [], 0.1)
        if master in readable:
            try:
                data = os.read(master, 65536)
            except OSError:
                break
            if not data:
                break
            print(json.dumps({'data': base64.b64encode(data).decode()}), flush=True)
        if sys.stdin in readable:
            line = sys.stdin.readline()
            if not line:
                break
            command = json.loads(line)
            if 'input' in command:
                os.write(master, command['input'].encode())
            elif 'cols' in command:
                fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', command['rows'], command['cols'], 0, 0))
            elif command.get('close'):
                break
finally:
    try:
        os.killpg(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    os.close(master)
    os.waitpid(pid, 0)
    print(json.dumps({'exited': True}), flush=True)
