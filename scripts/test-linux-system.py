#!/usr/bin/env python3
"""Acceptance against a disposable, installed Linux deployment (run as root).

Requires the production control/edge/client systemd units, a trusted TLS reverse
proxy, and an official gost 3.3.0 binary. It creates and removes a test client.
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import shlex
import ssl
import subprocess
import threading
import time
import urllib.error
import urllib.request


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--origin', default='https://127.0.0.1')
    p.add_argument('--ca', default='/etc/altasci/tls/fullchain.pem')
    p.add_argument('--gost', required=True)
    p.add_argument('--project', required=True)
    p.add_argument('--results', required=True)
    args = p.parse_args()
    results = Path(args.results)
    results.mkdir(parents=True, exist_ok=True)
    checks = []
    children = []
    ctx = ssl.create_default_context(cafile=args.ca)
    browser = urllib.request.build_opener(
        urllib.request.HTTPSHandler(context=ctx),
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ''

    def check(condition, message):
        if not condition:
            raise AssertionError(message)
        checks.append(message)
        print('PASS:', message, flush=True)

    def wait(fn, message, timeout=45):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                value = fn()
                if value:
                    return value
            except (OSError, ValueError, subprocess.SubprocessError):
                pass
            time.sleep(.2)
        raise AssertionError('Timeout: ' + message)

    def api(method, path, body=None):
        req = urllib.request.Request(args.origin + '/api/v1' + path,
            data=json.dumps(body).encode() if body is not None else None,
            headers={'Content-Type': 'application/json', 'X-CSRF-Token': csrf}, method=method)
        try:
            with browser.open(req, timeout=10) as res:
                return json.load(res)
        except urllib.error.HTTPError as e:
            if e.code >= 500:
                raise  # service restart/proxy readiness is retried by wait()
            raise AssertionError(f'{method} {path}: {e.code}: {e.read().decode()}') from e

    def run(*command):
        return subprocess.check_output(command, text=True, stderr=subprocess.STDOUT).strip()

    def status():
        return json.loads(run('/usr/local/bin/altascitunnel', 'status'))

    def pid(unit):
        return run('systemctl', 'show', '-p', 'MainPID', '--value', unit)

    def echo_tcp(listener):
        while True:
            conn, _ = listener.accept()
            def serve(c):
                with c:
                    while data := c.recv(65535):
                        c.sendall(data)
            threading.Thread(target=serve, args=(conn,), daemon=True).start()

    def echo_udp(listener):
        while True:
            data, peer = listener.recvfrom(65535)
            listener.sendto(data, peer)

    def connect_echo(protocol, port):
        s = socket.socket(type=socket.SOCK_STREAM if protocol == 'tcp' else socket.SOCK_DGRAM)
        s.settimeout(5)
        try:
            s.connect(('127.0.0.1', port))
            s.sendall(b'linux-installed-service')
            assert s.recv(100) == b'linux-installed-service'
            return s
        except Exception:
            s.close()
            raise

    def roundtrip(stage):
        for protocol, port in visitor_ports.items():
            conn = wait(lambda: connect_echo(protocol, port), stage + ' ' + protocol)
            conn.close()
            check(True, stage + ' ' + protocol + ' through TLS proxy and installed service')

    try:
        csrf = api('POST', '/auth/login', {'username': 'acceptance', 'password': os.environ['ACCEPTANCE_PASSWORD']})['csrf']
        check(status()['state'] == 'unregistered', 'new systemd service is unregistered')
        check(run('systemctl', 'show', '-p', 'User', '--value', 'altascitunnel') == 'altascitunnel', 'client runs as dedicated unprivileged user')
        check((Path('/var/lib/altascitunnel').stat().st_mode & 0o777) == 0o700, 'private client state directory mode 0700')
        check((Path('/run/altasci/edge.sock').stat().st_mode & 0o777) == 0o660, 'Edge control socket mode 0660')
        en = api('POST', '/enrollments', {'name': 'linux-installed-service'})
        run('/usr/local/bin/altascitunnel', 'register', '--server', args.origin.replace('https:', 'wss:'), '--token', en['token'])
        client_id = wait(lambda: json.loads(Path('/var/lib/altascitunnel/identity.json').read_text()).get('client_id'), 'assigned ClientID')
        check(bool(client_id), 'Ed25519 enrollment through installed service IPC and TLS proxy')
        identity = json.loads(Path('/var/lib/altascitunnel/identity.json').read_text())
        visitor_ports, credentials = {}, []
        for protocol in ('tcp', 'udp'):
            echo = socket.socket(type=socket.SOCK_STREAM if protocol == 'tcp' else socket.SOCK_DGRAM)
            echo.bind(('127.0.0.1', 0))
            if protocol == 'tcp':
                echo.listen()
            threading.Thread(target=echo_tcp if protocol == 'tcp' else echo_udp, args=(echo,), daemon=True).start()
            op = api('POST', f'/clients/{client_id}/tunnels', {'protocol': protocol, 'target_ip': '127.0.0.1', 'target_port': echo.getsockname()[1]})
            tid = op['resource_id']
            cred = api('POST', f'/tunnels/{tid}/access-credentials', {})['resource_id']
            credentials.append(cred)
            secret = api('POST', f'/access-credentials/{cred}/reveal', {})
            probe = socket.socket()
            probe.bind(('127.0.0.1', 0))
            port = probe.getsockname()[1]
            probe.close()
            visitor_ports[protocol] = port
            listen = f'{protocol}://127.0.0.1:{port}/203.0.113.9:65000'
            if protocol == 'udp':
                listen += '?keepalive=true&ttl=60s'
            exported = shlex.split(secret['command'])
            forward = exported[exported.index('-F') + 1] + '&caFile=' + args.ca
            log = open(results / f'visitor-{protocol}.log', 'w')
            children.append(subprocess.Popen([args.gost, '-L', listen, '-F', forward], stdout=log, stderr=log))
        roundtrip('initial')
        live = connect_echo('tcp', visitor_ports['tcp'])
        run('systemctl', 'stop', 'altasci-control')
        live.sendall(b'control-stopped')
        check(live.recv(100) == b'control-stopped', 'existing TCP survives stopped control service')
        run('systemctl', 'start', 'altasci-control')
        # The session is in PostgreSQL, so the same login remains valid.
        wait(lambda: api('GET', '/clients'), 'control service restart')
        csrf = api('POST', '/auth/login', {'username': 'acceptance', 'password': os.environ['ACCEPTANCE_PASSWORD']})['csrf']
        old = pid('altascitunnel')
        run('systemctl', 'kill', '-s', 'SIGKILL', 'altascitunnel')
        wait(lambda: pid('altascitunnel') not in ('0', old), 'systemd client crash recovery')
        roundtrip('client crash recovery')
        check(json.loads(Path('/var/lib/altascitunnel/identity.json').read_text())['private_key'] == identity['private_key'], 'crash recovery preserves private key')
        old = pid('altascitunnel')
        run('sh', str(Path(args.project) / 'scripts/install-linux.sh'), str(Path(args.project) / 'bin/linux-amd64/altascitunnel'))
        check(pid('altascitunnel') not in ('0', old), 'install over running service restarts the updated executable')
        roundtrip('upgrade')
        check(json.loads(Path('/var/lib/altascitunnel/identity.json').read_text()) == identity, 'upgrade preserves registered identity')
        run('systemctl', 'restart', 'altasci-edge')
        roundtrip('Edge restart with cached policy')
        live.close()
        live = connect_echo('tcp', visitor_ports['tcp'])
        api('DELETE', f'/access-credentials/{credentials[0]}', {})
        live.settimeout(10)
        try:
            closed = live.recv(1) == b''
        except ConnectionResetError:
            closed = True
        live.close()
        check(closed, 'revoking Visitor closes existing installed-service data connection')
        run('sh', str(Path(args.project) / 'scripts/uninstall-linux.sh'))
        check(not Path('/usr/local/bin/altascitunnel').exists() and not Path('/etc/systemd/system/altascitunnel.service').exists(), 'uninstall removes binary and unit')
        check(json.loads(Path('/var/lib/altascitunnel/identity.json').read_text()) == identity, 'ordinary uninstall retains identity')
        run('sh', str(Path(args.project) / 'scripts/install-linux.sh'), str(Path(args.project) / 'bin/linux-amd64/altascitunnel'))
        conn = wait(lambda: connect_echo('udp', visitor_ports['udp']), 'reinstall resumes UDP'); conn.close()
        check(True, 'reinstall resumes forwarding without reenrollment')
        run('sh', str(Path(args.project) / 'scripts/uninstall-linux.sh'), '--purge')
        check(not Path('/var/lib/altascitunnel').exists(), 'purge removes identity and all client state')
        (results / 'result.json').write_text(json.dumps({'passed': True, 'checks': checks}, indent=2))
    except Exception as e:
        (results / 'result.json').write_text(json.dumps({'passed': False, 'checks': checks, 'error': str(e)}, indent=2))
        raise
    finally:
        for child in children:
            child.terminate()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill(); child.wait()


if __name__ == '__main__':
    main()
