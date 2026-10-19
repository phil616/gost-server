#!/usr/bin/env python3
"""Exercise a built server image using isolated Docker resources and a test DB.

PostgreSQL is a separate test fixture, never part of the server image or Compose.
No project .env/compose.env or existing database is read. Standard library only.
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def docker(*args, input_text=None, check=True):
    result = subprocess.run(
        ["docker", *args], input=input_text, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180,
    )
    if check and result.returncode:
        # Commands contain isolated fixture values only, never user credentials.
        raise RuntimeError(f"docker {args[0]} failed: {result.stderr.strip()}")
    return result


def eventually(check, timeout=60):
    deadline = time.monotonic() + timeout
    error = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except (OSError, ValueError, RuntimeError) as exc:
            error = exc
        time.sleep(0.3)
    raise AssertionError(f"Timed out waiting for test condition: {error}")


def inspect(name):
    return json.loads(docker("inspect", name).stdout)[0]


def expect_failed(*args):
    if docker(*args, check=False).returncode == 0:
        raise AssertionError("Expected command to fail")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="altasci-server:dev")
    parser.add_argument("--postgres-image", default="postgres:18")
    args = parser.parse_args()
    if sys.platform != "linux" or os.geteuid() != 0:
        parser.error("run as root on the Linux host of a local Docker daemon (e.g. sudo python3)")
    daemon_host = os.environ.get("DOCKER_HOST") or json.loads(
        docker("context", "inspect", "--format", "{{json .Endpoints.docker.Host}}").stdout)
    if not daemon_host.startswith("unix://"):
        parser.error("the process lifecycle test requires a local Unix-socket Docker daemon")
    suffix = uuid.uuid4().hex[:12]
    prefix = "altasci-test-" + suffix
    network, volume, database, server = [prefix + x for x in ("-net", "-data", "-db", "-server")]
    password = "test-only-" + uuid.uuid4().hex + "$literal"
    origin = "https://docker-test.example:18443"
    dsn = f"postgres://postgres:{password}@{database}:5432/postgres?sslmode=disable"
    admin_password = "isolated-admin-" + uuid.uuid4().hex
    created_containers = []
    image_info = json.loads(docker("image", "inspect", args.image).stdout)[0]
    assert image_info["Config"]["User"] == "65532:65532"
    assert image_info["Config"]["Entrypoint"] == ["/usr/local/bin/altasci-server"]
    common = [
        "--network", network, "-e", "DATABASE_URL=" + dsn,
        "-e", "ALTASCI_PUBLIC_URL=" + origin,
        "--mount", f"source={volume},target=/data",
        "--tmpfs", "/run/altasci:uid=65532,gid=65532,mode=0700",
        "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
    ]

    def once(*command, input_text=None, check=True):
        return docker("run", "--rm", "-i", *common, args.image, *command,
                      input_text=input_text, check=check)

    def start():
        docker("run", "-d", "--name", server, *common,
               "-p", "127.0.0.1::18081", "-p", "127.0.0.1::18080",
               "--health-interval=1s", "--health-start-period=1s", "--stop-timeout=25",
               args.image)
        if server not in created_containers:
            created_containers.append(server)
        eventually(lambda: inspect(server)["State"].get("Health", {}).get("Status") == "healthy")
        ports = inspect(server)["NetworkSettings"]["Ports"]
        for bindings in ports.values():
            assert bindings and all(item["HostIp"] == "127.0.0.1" for item in bindings)
        return "http://127.0.0.1:" + ports["18081/tcp"][0]["HostPort"]

    def kill_edge(name):
        processes = docker("top", name, "-eo", "pid,args").stdout.splitlines()
        matches = [line for line in processes if "/usr/local/bin/altasci-edge " in line]
        assert len(matches) == 1, processes
        edge_pid = int(matches[0].strip().split()[0])
        assert os.readlink(f"/proc/{edge_pid}/exe") == "/usr/local/bin/altasci-edge"
        assert inspect(name)["Id"] in Path(f"/proc/{edge_pid}/cgroup").read_text()
        os.kill(edge_pid, 9)

    try:
        docker("network", "create", network)
        docker("volume", "create", volume)
        docker("run", "-d", "--name", database, "--network", network,
               "-e", "POSTGRES_PASSWORD=" + password, args.postgres_image)
        created_containers.append(database)
        eventually(lambda: docker("exec", database, "pg_isready", "-U", "postgres", check=False).returncode == 0)

        # serve refuses an uninitialized volume; init creates and retains its key.
        assert once("serve", check=False).returncode != 0
        once("init")
        print("PASS: external PostgreSQL migration and nonroot volume initialization")

        with tempfile.TemporaryDirectory(prefix=prefix) as directory:
            root = Path(directory)
            reader = docker("create", *common, args.image).stdout.strip()
            created_containers.append(reader)
            docker("cp", f"{reader}:/data/secrets.key", str(root / "key-before"))
            key_before = (root / "key-before").read_bytes()
            assert len(key_before) == 32
            once("init")
            expect_failed("run", "--rm", *common, args.image, "keygen")
            docker("cp", f"{reader}:/data/secrets.key", str(root / "key-after"))
            assert (root / "key-after").read_bytes() == key_before

            # Validate actual legal payload, checksum-verified MPL sources and
            # rebuild source, while checking caches/secrets/agent are absent.
            docker("cp", f"{reader}:/usr/share/altasci", str(root / "legal"))
            legal = root / "legal"
            assert "@phil616" in (legal / "LICENSE").read_text()
            inventory = json.loads((legal / "licenses/dependency-inventory.json").read_text())
            mpl_count = 0
            for dependency in inventory["dependencies"]:
                if dependency["license"] == "MPL-2.0":
                    source = legal / "licenses" / dependency["sourceArchive"]
                    assert hashlib.sha256(source.read_bytes()).hexdigest() == dependency["sourceSHA256"]
                    mpl_count += 1
            assert mpl_count >= 2
            with tarfile.open(legal / "sources/project-source.tar.gz") as archive:
                names = archive.getnames()
                assert "Dockerfile" in names and "cmd/altasci-server/main.go" in names
                assert not any("node_modules/" in n or n.startswith("web/dist/") or n.endswith((".env", ".key", ".pem", ".pfx")) for n in names)
            expect_failed("cp", f"{reader}:/usr/local/bin/altascitunnel", str(root / "agent"))
            expect_failed("cp", f"{reader}:/bin/sh", str(root / "shell"))
            print(f"PASS: retained key, legal payload ({len(inventory['dependencies'])} components), MPL sources, clean source archive; no client/shell")

            once("admin-create", "--username", "docker-admin", "--password-stdin", input_text=admin_password + "\n")
            endpoint = start()
            with urllib.request.urlopen(endpoint + "/", timeout=5) as response:
                assert b"<html" in response.read().lower()
            with urllib.request.urlopen(endpoint + "/health/ready", timeout=5) as response:
                assert response.status == 200

            # A local plaintext probe cannot send Secure cookies automatically.
            # Copy this isolated test cookie explicitly; real access uses HTTPS.
            jar = http.cookiejar.CookieJar()
            browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
            login = urllib.request.Request(endpoint + "/api/v1/auth/login",
                data=json.dumps({"username": "docker-admin", "password": admin_password}).encode(),
                headers={"Content-Type": "application/json", "Host": "docker-test.example:18443", "Origin": origin})
            with browser.open(login, timeout=15) as response:
                session = json.load(response)
                assert "Secure" in response.headers.get("Set-Cookie", "")
            cookie = "; ".join(f"{item.name}={item.value}" for item in jar)
            headers = {"Cookie": cookie, "Content-Type": "application/json", "Origin": origin,
                       "Host": "docker-test.example:18443", "X-CSRF-Token": session["csrf"], "Idempotency-Key": str(uuid.uuid4())}
            enrollment = urllib.request.Request(endpoint + "/api/v1/enrollments",
                data=b'{"name":"container-check"}', headers=headers)
            with browser.open(enrollment, timeout=10) as response:
                assert origin.replace("https://", "wss://") in json.load(response)["command"]
            edge_status = urllib.request.Request(endpoint + "/api/v1/edge/status", headers=headers)
            def synchronized():
                with browser.open(edge_status, timeout=10) as response:
                    status = json.load(response)
                return status["healthy"] and status["runtime"]["revision"] >= 1
            eventually(synchronized)
            print("PASS: Web, database readiness, secure-cookie login, CSRF enrollment and Unix-socket policy coordination")

            docker("stop", "--time", "25", server)
            state = inspect(server)["State"]
            assert state["ExitCode"] == 0 and not state["OOMKilled"]
            docker("rm", server)
            endpoint = start()
            docker("cp", f"{server}:/data/secrets.key", str(root / "key-recreated"))
            assert (root / "key-recreated").read_bytes() == key_before
            docker("cp", f"{server}:/data/policy.json", str(root / "policy.json"))
            assert json.loads((root / "policy.json").read_text())["revision"] >= 1
            print("PASS: SIGTERM clean exit and container recreation preserve key and policy snapshot")

            # Kill a child from the host PID namespace; do not add a shell or
            # process-killing utility to the production image.
            kill_edge(server)
            eventually(lambda: not inspect(server)["State"]["Running"])
            assert inspect(server)["State"]["ExitCode"] != 0
            print("PASS: killing Edge stops Control and fails the container for restart supervision")

            # Exercise the actual checked-in Compose service. The override only
            # points it at our isolated DB/network/volume and already-built tag.
            repository = Path(__file__).resolve().parents[1]
            environment = root / "fixture.env"
            environment.write_text(
                f"DATABASE_URL='{dsn}'\nALTASCI_PUBLIC_URL={origin}\n"
                "ALTASCI_CONTROL_PORT=0\nALTASCI_EDGE_PORT=0\n")
            override = root / "override.json"
            override.write_text(json.dumps({
                "services": {"server": {"image": args.image}},
                "networks": {"default": {"external": True, "name": network}},
                "volumes": {"server-data": {"external": True, "name": volume}},
            }))
            compose_prefix = ["compose", "--project-directory", str(repository),
                "--env-file", str(environment), "-p", prefix + "-compose",
                "-f", str(repository / "compose.yaml"), "-f", str(override)]
            try:
                docker(*compose_prefix, "run", "--rm", "--no-deps", "-T", "server", "migrate")
                docker(*compose_prefix, "up", "-d", "--no-build", "server")
                compose_server = docker(*compose_prefix, "ps", "-q", "server").stdout.strip()
                assert compose_server
                eventually(lambda: inspect(compose_server)["State"].get("Health", {}).get("Status") == "healthy")
                assert inspect(compose_server)["HostConfig"]["ReadonlyRootfs"]
                assert inspect(compose_server)["HostConfig"]["RestartPolicy"]["Name"] == "unless-stopped"
                bindings = inspect(compose_server)["NetworkSettings"]["Ports"]
                assert all(binding["HostIp"] == "127.0.0.1" for port in bindings.values() for binding in port)
                kill_edge(compose_server)
                eventually(lambda: inspect(compose_server)["RestartCount"] >= 1)
                eventually(lambda: inspect(compose_server)["State"].get("Health", {}).get("Status") == "healthy")
                docker(*compose_prefix, "stop", "server")
                assert inspect(compose_server)["State"]["ExitCode"] == 0
                print("PASS: real Compose env file with literal $, migration, private ports, readonly root and automatic crash restart")
            finally:
                docker(*compose_prefix, "down", check=False)

        # A reachable DB with a missing socket must not yield a false healthy.
        assert once("healthcheck", check=False).returncode != 0
        print("PASS: healthcheck rejects unavailable services")
    finally:
        for name in reversed(created_containers):
            docker("rm", "-f", "-v", name, check=False)
        docker("volume", "rm", volume, check=False)
        docker("network", "rm", network, check=False)


if __name__ == "__main__":
    main()
