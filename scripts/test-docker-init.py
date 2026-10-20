#!/usr/bin/env python3
"""Test host-side initialization with isolated Docker resources; no production config."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid
import urllib.error
import urllib.request


def docker(*args, check=True, input_text=None):
    result = subprocess.run(
        ["docker", *args], input=input_text, text=True, capture_output=True, timeout=180,
    )
    if check and result.returncode:
        raise AssertionError(result.stderr)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="altasci-server:dev")
    parser.add_argument("--postgres-image", default="postgres:18")
    args = parser.parse_args()
    script = Path(__file__).resolve().with_name("init-docker.sh")
    prefix = "altasci-init-test-" + uuid.uuid4().hex[:12]
    network, database, volume, empty_volume = [prefix + x for x in ("-net", "-db", "-data", "-empty")]
    server = prefix + "-server"
    password = "db-" + uuid.uuid4().hex
    admin_password = "test-$literal-`backticks`-$(touch /tmp/never)-'quoted'-\"double\""
    docker("network", "create", network)
    try:
        docker("run", "-d", "--name", database, "--network", network,
               "-e", "POSTGRES_PASSWORD=" + password, args.postgres_image)
        for _ in range(60):
            if docker("exec", database, "pg_isready", "-U", "postgres", check=False).returncode == 0:
                break
            time.sleep(0.5)
        else:
            raise AssertionError("Test database did not become ready")

        def query(sql):
            return docker("exec", database, "psql", "-U", "postgres", "-qAt",
                          "--set=ON_ERROR_STOP=1", "--command", sql).stdout.strip()

        def volume_shell(command):
            return docker("run", "--rm", "--network", "none", "--user", "65532:65532",
                          "--mount", f"type=volume,src={volume},dst=/data",
                          "--entrypoint", "/bin/sh", args.postgres_image, "-ec", command).stdout.strip()

        with tempfile.TemporaryDirectory(prefix=prefix) as temp:
            env_file = Path(temp) / "app.env"
            lines = [
                f"DATABASE_URL=postgres://postgres:{password}@{database}:5432/postgres?sslmode=disable",
                "ALTASCI_PUBLIC_URL=https://init-test.example",
                "ALTASCI_INIT_ADMIN_USERNAME=initial-admin",
            ]

            def write_env(admin_secret):
                env_file.write_text("\n".join(lines + ["ALTASCI_INIT_ADMIN_PASSWORD=" + admin_secret]) + "\n")
                env_file.chmod(0o600)

            environment = dict(os.environ, ALTASCI_DEPLOY_LOCK_DIR=temp)

            def initialize(target_volume=volume, success=True):
                result = subprocess.run(
                    ["bash", str(script), args.image, str(env_file), target_volume,
                     network, args.postgres_image],
                    env=environment, text=True, capture_output=True, timeout=180,
                )
                assert (result.returncode == 0) == success, result.stdout + result.stderr
                assert admin_password not in result.stdout + result.stderr
                return result

            # Migration/key creation can finish even when admin creation fails.
            write_env("short")
            failed = initialize(success=False)
            assert "password must contain at least 12" in failed.stderr, failed.stdout + failed.stderr
            assert volume_shell("test ! -e /data/.initialized; wc -c < /data/secrets.key") == "32"
            assert query("SELECT count(*) FROM admins;") == "0"
            key_before = volume_shell("sha256sum /data/secrets.key").split()[0]
            print("PASS: failed initialization does not write a marker")

            write_env(admin_password)
            initialize()
            assert volume_shell("cat /data/.initialized") == "1"
            snapshot = query("SELECT md5(payload::text) FROM admins;")
            assert query("SELECT count(*) FROM admins;") == "1"
            assert volume_shell("sha256sum /data/secrets.key").split()[0] == key_before

            # Verify shell metacharacters in the env file reach the real login API literally.
            docker("run", "-d", "--name", server, "--network", network,
                   "--env-file", str(env_file), "--env", "ALTASCI_DATA_DIR=/data",
                   "--mount", f"type=volume,src={volume},dst=/data",
                   "--tmpfs", "/run/altasci:uid=65532,gid=65532,mode=0700",
                   "-p", "127.0.0.1::18081", args.image)
            port = docker("port", server, "18081/tcp").stdout.strip().rsplit(":", 1)[1]
            endpoint = "http://127.0.0.1:" + port
            for _ in range(60):
                try:
                    with urllib.request.urlopen(endpoint + "/health/ready", timeout=2) as response:
                        assert response.status == 200
                    break
                except (OSError, urllib.error.URLError):
                    time.sleep(0.5)
            else:
                raise AssertionError("Test server did not become ready")
            request = urllib.request.Request(
                endpoint + "/api/v1/auth/login",
                data=json.dumps({"username": "initial-admin", "password": admin_password}).encode(),
                headers={"Content-Type": "application/json", "Origin": "https://init-test.example"},
            )
            with urllib.request.urlopen(request, timeout=15) as response:
                assert response.status == 200
            docker("rm", "-f", server)
            print("PASS: passwords containing shell metacharacters work at the login API")
            print("PASS: retry creates one administrator and preserves the key")

            # Marker loss after commit must not recreate/reset the administrator.
            volume_shell("rm /data/.initialized")
            write_env("another-strong-password")
            initialize()
            assert query("SELECT md5(payload::text) FROM admins;") == snapshot
            assert volume_shell("cat /data/.initialized") == "1"
            initialize()
            assert query("SELECT md5(payload::text) FROM admins;") == snapshot
            print("PASS: missing/existing marker retries preserve administrator credentials")

            # Concurrent attempts on this host/volume share the initialization lock.
            volume_shell("rm /data/.initialized")
            command = ["bash", str(script), args.image, str(env_file), volume, network, args.postgres_image]
            processes = [subprocess.Popen(command, env=environment, stdout=subprocess.PIPE,
                                          stderr=subprocess.PIPE, text=True) for _ in range(2)]
            for process in processes:
                stdout, stderr = process.communicate(timeout=180)
                assert process.returncode == 0, stdout + stderr
            assert query("SELECT md5(payload::text) FROM admins;") == snapshot
            print("PASS: concurrent initializations serialize")

            env_file.write_text("\n".join(lines[:2]) + "\n")
            initialize()
            print("PASS: initialized deployments no longer require admin secrets")

            volume_shell("cp /data/secrets.key /data/key.saved; printf broken > /data/secrets.key")
            initialize(success=False)
            volume_shell("mv /data/key.saved /data/secrets.key")
            print("PASS: a damaged key is rejected even when the marker exists")

            # A populated database with a fresh volume must not get a new key.
            initialize(target_volume=empty_volume, success=False)
            assert query("SELECT md5(payload::text) FROM admins;") == snapshot
            print("PASS: existing database without its key is rejected")

            # A stale marker must not bootstrap a replacement database.
            query("CREATE DATABASE replacement;")
            env_file.write_text(env_file.read_text().replace("/postgres?", "/replacement?"))
            initialize(success=False)
            print("PASS: stale marker paired with an empty database is rejected")
    finally:
        docker("rm", "-f", server, check=False)
        docker("rm", "-f", "--volumes", database, check=False)
        for name in (volume, empty_volume):
            docker("volume", "rm", name, check=False)
        docker("network", "rm", network, check=False)


if __name__ == "__main__":
    main()
