"""One owned Linux install/recovery/update cycle; no real provider credentials.

Arguments: current bundle, fixed schema-22 API binary, TLS directory.
All Docker projects and files are newly created for this fixture and retained.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile

bundle, previous_api, tls = (Path(p).resolve() for p in sys.argv[1:4])
root = Path(tempfile.mkdtemp(prefix="koujou-distribution-"))
cli = str(bundle / "koujou")
Path(cli).chmod(0o755)
script = str(Path(__file__).with_name("test-distribution.py"))
deployments = []


def run(args, success=True):
    result = subprocess.run(list(map(str, args)), capture_output=True)
    if (result.returncode == 0) != success:
        diagnostic = root / "command-failure.log"
        diagnostic.write_bytes(result.stdout + result.stderr)
        diagnostic.chmod(0o600)
        raise RuntimeError("owned command failed: " + str(args[:2]))
    return result.stdout


def compose(state, *args):
    return ["docker", "compose", "--project-directory", state, "-f", state / "compose.json", *args]


def sql(state, statement):
    return run(compose(state, "exec", "-T", "postgres", "psql", "-U", "postgres", "-d", "koujou", "-At", "-v", "ON_ERROR_STOP=1", "-c", statement)).decode().strip()


def initialize(state, release):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", int(os.environ.get("KOUJOU_TEST_DISTRIBUTION_PORT", "51429"))))
        port = listener.getsockname()[1]
    run([cli, "init", "--state", state, "--bundle", release, "--url", "https://127.0.0.1:" + str(port),
         "--smtp-file", root / "smtp", "--cert", tls / "cert.pem", "--key", tls / "key.pem", "--ca", tls / "cert.pem"])
    deployments.append(state)
    run([cli, "up", "--state", state])
    run([sys.executable, script, state, bundle])
    print("PASS: fresh packaged installation and two-member API fixture", flush=True)


def verify(state, fixture):
    output = run([sys.executable, script, state, bundle, "verify", fixture])
    print(output.decode().strip(), flush=True)


try:
    (root / "smtp").write_text("smtp://mailpit:1025/?disable_starttls=true")
    (root / "smtp").chmod(0o600)
    if "--updates-only" not in sys.argv[4:]:
        current = root / "current"
        initialize(current, bundle)
        run([cli, "bootstrap", "--state", current], success=False)
        assert not (current / ".operation").exists(), "failed operation retained its lock"
        # Every private service stays unpublished in the actual Docker configuration.
        config = json.loads((current / "compose.json").read_text())
        for service, spec in config["services"].items():
            if service != "edge":
                assert not spec.get("ports"), "private service exposed"
        backup = root / "backup.tgz"
        run([cli, "backup", "--state", current, "--backup", backup])
        assert backup.stat().st_mode & 0o777 == 0o600
        run([cli, "stop", "--state", current])
        restored = root / "restored"
        deployments.append(restored)
        run([cli, "restore", "--state", restored, "--bundle", bundle, "--backup", backup])
        running = run(compose(restored, "ps", "--status", "running", "--services")).decode().split()
        assert running == ["postgres"], "restore implicitly started writers"
        assert not any((restored / "search").iterdir()), "restored a stale search projection"
        run([cli, "up", "--state", restored])
        verify(restored, current)
        run([cli, "up", "--state", restored, "--llm"])
        verify(restored, current)
        run([cli, "backup", "--state", restored, "--backup", root / "gateway-backup.tgz"])
        run([cli, "stop", "--state", restored])
        print("PASS: private backup, data-only restore, rebuilt projections and optional empty-provider Gateway", flush=True)

    # This is an owned compatibility fixture, not a previously released package:
    # actual #27 API 17def2aa / schema 22 + current deployment edge and templates.
    old_bundle = root / "schema22-bundle"
    old_bundle.mkdir()
    shutil.copytree(bundle / "templates", old_bundle / "templates")
    shutil.copy(previous_api, old_bundle / "api")
    # Current stdin command entry against the same schema-22 persistence source.
    shutil.copy(previous_api.with_name(previous_api.name.replace("-api", "-auth-bootstrap")), old_bundle / "auth-bootstrap")
    release = json.loads((bundle / "release.json").read_text())
    old_image = "koujou-owned-update:" + root.name
    (old_bundle / "Dockerfile").write_text("FROM " + release["image"] + "\nCOPY api auth-bootstrap /app/\n")
    run(["docker", "build", "--platform", "linux/amd64", "-t", old_image, old_bundle])
    run(["docker", "save", "-o", old_bundle / "server-image.tar", old_image])
    release.update(image=old_image, image_id=run(["docker", "image", "inspect", "--format", "{{.Id}}", old_image]).decode().strip(),
                   schema=22, commit="owned-17def2aa-api-with-current-edge", version="owned-schema22-fixture")
    release["files"] = {}
    for name in ("server-image.tar", "templates/identity.schema.json", "templates/postgres-init.sh", "templates/bifrost.json"):
        with (old_bundle / name).open("rb") as source:
            digest = hashlib.sha256()
            for block in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(block)
            release["files"][name] = digest.hexdigest()
    (old_bundle / "release.json").write_text(json.dumps(release))
    old = root / "schema22"
    initialize(old, old_bundle)
    assert sql(old, "SELECT max(version) FROM shared_migrations") == "22"
    sql(old, "CREATE FUNCTION owned_reject_migration() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'owned migration failure'; END $$; CREATE EVENT TRIGGER owned_reject_migration ON ddl_command_end WHEN TAG IN ('CREATE TABLE') EXECUTE FUNCTION owned_reject_migration();")
    run([cli, "update", "--state", old, "--bundle", bundle], success=False)
    assert not (old / ".operation").exists()
    assert json.loads((old / "settings.json").read_text())["release"]["schema"] == 22
    assert sql(old, "SELECT max(version) FROM shared_migrations") == "22"
    verify(old, old)
    sql(old, "DROP EVENT TRIGGER owned_reject_migration; DROP FUNCTION owned_reject_migration();")
    run([cli, "update", "--state", old, "--bundle", bundle])
    assert sql(old, "SELECT max(version) FROM shared_migrations") == "23"
    verify(old, old)
    print("PASS: schema 22→23 real migration; failed migration restores original release/three databases/sessions; retry succeeds", flush=True)
finally:
    for deployment in deployments:
        if (deployment / "compose.json").exists():
            run([cli, "stop", "--state", deployment])
    print("Owned fixture retained at " + str(root), flush=True)
