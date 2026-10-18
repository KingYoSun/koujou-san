"""Owned Linux distribution acceptance; uses only synthetic local data.

Run after `koujou init/up`, with STATE and BUNDLE arguments. No provider or
GitHub credentials are used. State must be an explicitly disposable fixture.
"""
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

state, bundle = map(lambda p: Path(p).resolve(), sys.argv[1:3])
settings = json.loads((state / "settings.json").read_text())
site = settings["site"]
compose = ["docker", "compose", "--project-directory", str(state), "-f", str(state / "compose.json")]
project = json.loads((state / "compose.json").read_text())["name"]
cli = [str(bundle / "koujou")]


def run(args, **kwargs):
    result = subprocess.run(args, capture_output=True, **kwargs)
    if result.returncode:
        raise RuntimeError("owned subprocess failed: " + " ".join(args[:3]))
    return result.stdout


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


class Browser:
    def __init__(self):
        context = ssl.create_default_context(cafile=str(state / "config/ca.pem"))
        self.jar = http.cookiejar.LWPCookieJar()
        self.open = urllib.request.build_opener(
            urllib.request.HTTPSHandler(context=context),
            urllib.request.HTTPCookieProcessor(self.jar), NoRedirect())

    def request(self, method, path, value=None, form=False):
        target = path if path.startswith("https://") else site + path
        assert target.startswith(site + "/"), "fixture may only use its local site"
        headers = {"Origin": site}
        data = None
        if value is not None:
            data = (urllib.parse.urlencode(value) if form else json.dumps(value)).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded" if form else "application/json"
        if method != "GET" and not path.startswith("/auth/flow/"):
            _, _, token = self.request("GET", "/auth/csrf")
            headers["X-CSRF-Token"] = token["csrf"]
        try:
            response = self.open.open(urllib.request.Request(target, data, headers, method=method), timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            raw = response.read()
            body = json.loads(raw) if "application/json" in response.headers.get("Content-Type", "") else None
            return response.status, response.headers, body

    def json(self, method, path, value=None):
        status, _, body = self.request(method, path, value)
        assert status == 200, f"{method} {path.split('?')[0]} returned {status}"
        assert body is not None and not body.get("error"), "API result missing or rejected"
        return body

    def flow(self, kind, fields, query=""):
        flow = self.json("GET", "/auth/flow/" + kind + ("?" + query if query else ""))
        for node in flow["ui"]["nodes"]:
            attr = node["attributes"]
            if attr.get("type") == "hidden":
                fields[attr["name"]] = attr.get("value", "")
        status, headers, _ = self.request("POST", "/auth/flow/" + kind + "?flow=" + flow["id"], fields, True)
        assert status == 303, kind + " submission failed"
        return flow["id"], headers.get("Location")

    def login(self, email, password):
        self.flow("login", {"method": "password", "identifier": email, "password": password})
        _, headers, _ = self.request("GET", "/auth/start")
        next_url = headers["Location"]
        for _ in range(20):
            target = urllib.parse.urlsplit(next_url)
            if target.path in ("/auth/login", "/auth/consent"):
                kind = target.path.rsplit("/", 1)[1]
                challenge = urllib.parse.parse_qs(target.query)[kind + "_challenge"][0]
                result = self.json("POST", "/auth/broker/" + kind + "?challenge=" + urllib.parse.quote(challenge))
                if result.get("required") == "login":
                    _, next_url = self.flow("login", {"method": "password", "identifier": email, "password": password},
                                           "login_challenge=" + urllib.parse.quote(challenge))
                    continue
                assert not result.get("required"), "login requirement: " + str(result.get("required"))
                next_url = result["redirect"]
            else:
                status, headers, _ = self.request("GET", next_url)
                assert status in (302, 303), "authorization did not redirect: " + target.path + " " + str(status)
                if target.path == "/auth/callback":
                    return self.json("GET", "/auth/me")
                next_url = headers["Location"]
        raise AssertionError("authorization exceeded fixture bound")


def mutation(version="0", **value):
    return {"operation_id": secrets.token_hex(16), "expected_version": version, **value}


used_messages = set()


def mail_code(address):
    for _ in range(150):
        messages = json.loads(urllib.request.urlopen(mail_url + "/api/v1/messages").read())
        for message in messages["messages"]:
            if message["ID"] not in used_messages and any(to["Address"] == address for to in message["To"]) and "verify" in message["Subject"].lower():
                content = json.loads(urllib.request.urlopen(mail_url + "/api/v1/message/" + message["ID"]).read())
                for word in content["Text"].split():
                    word = word.strip(".,:;\r\n")
                    if len(word) == 6 and word.isascii() and word.isdigit():
                        used_messages.add(message["ID"])
                        return word
        time.sleep(.1)
    raise AssertionError("local verification mail not delivered")


def verify(fixture_root):
    fixture = json.loads((fixture_root / "acceptance.json").read_text())
    for index, identity in enumerate(fixture["identities"]):
        browser = Browser()
        browser.jar.load(str(fixture_root / ("acceptance-cookies-" + str(index))), ignore_discard=True)
        assert browser.json("GET", "/auth/me") == identity, "restored authentication differs"
    for path, expected in fixture["records"].items():
        actual = browser.json("GET", path)
        assert actual == expected, "restored fixed record differs: " + path.split("?")[0]
    workspace = fixture["workspace"]
    assert len(browser.json("GET", "/access/workspaces/" + workspace + "/members")["data"]["items"]) == 2
    for engine, query in fixture["projections"].items():
        for _ in range(60):
            status, _, result = browser.request("POST", "/" + engine + "/workspaces/" + workspace + "/query", query)
            if status == 503:
                time.sleep(.5)
                continue
            assert status == 200 and result is not None and not result.get("error"), engine + " query failed"
            evidence = result.get("data", {}).get("evidence", {})
            if result["meta"]["completeness"]["state"] == "complete" and evidence.get("position") == evidence.get("required_position"):
                break
            time.sleep(.5)
        else:
            raise AssertionError(engine + " projection did not catch up")
        assert result["data"]["items"], engine + " projection returned no fixture data"
        normalized = {"items": result["data"]["items"], "position": evidence["position"]}
        if engine in fixture.get("projection_results", {}):
            assert normalized == fixture["projection_results"][engine], engine + " rebuilt result differs"
        fixture.setdefault("projection_results", {})[engine] = normalized
    (fixture_root / "acceptance.json").write_text(json.dumps(fixture))
    print("PASS: both restored BFF sessions, fixed records/history, members, Bleve/ArcadeDB results and source positions")


if len(sys.argv) > 3 and sys.argv[3] == "verify":
    verify(Path(sys.argv[4]))
    sys.exit(0)

# Local test inbox is separate from the product compose and has no mail relay.
mail_name = project + "-acceptance-mail"
run(["docker", "run", "--detach", "--name", mail_name, "--network", project + "_default",
     "--network-alias", "mailpit", "--publish", "127.0.0.1::8025", "axllent/mailpit:v1.31.5"])
mail_port = run(["docker", "port", mail_name, "8025"]).decode().strip().rsplit(":", 1)[1]
mail_url = "http://127.0.0.1:" + mail_port
try:
    fixture_id = secrets.token_hex(4)
    credential_file = state / "acceptance-password"
    password = "Fixture-" + secrets.token_urlsafe(24) + "aA1!"
    credential_file.write_text(password)
    credential_file.chmod(0o600)
    run(cli + ["bootstrap", "--state", str(state), "--admin-email", "distribution-admin-" + fixture_id + "@koujou.test",
               "--password-file", str(credential_file)])
    browsers, identities = [], []
    for role in ("admin", "member"):
        browser = Browser()
        email = "distribution-" + role + "-" + fixture_id + "@koujou.test"
        if role == "member":
            browser.flow("registration", {"method": "password", "traits.email": email, "password": password})
        flow_id, _ = browser.flow("verification", {"method": "code", "email": email})
        for _ in range(3):
            browser.flow("verification", {"method": "code", "code": mail_code(email)}, "flow=" + flow_id)
            if browser.json("GET", "/auth/flow/verification?flow=" + flow_id).get("state") == "passed_challenge":
                break
        else:
            raise AssertionError("verification did not complete")
        identities.append(browser.login(email, password))
        browsers.append(browser)
    admin, member = browsers
    workspace = "distribution-" + fixture_id
    prefix = "/access/workspaces/" + workspace
    admin.json("POST", prefix + "/create", mutation(name="Distribution acceptance"))
    invitation = admin.json("POST", prefix + "/invite-create", mutation(id="invitation-" + fixture_id, role="editor"))["invitation"]
    token = invitation.split(".", 1)[1]
    preview = member.json("POST", prefix + "/invite-preview", {"token": token})
    member.json("POST", prefix + "/invite-accept", mutation(preview["version"], token=token))
    assert len(member.json("GET", prefix + "/members")["data"]["items"]) == 2
    records, historical = {}, []
    for kind, identifier, value in [
        ("sentence", "source", {"kind": "sentence", "text": "Distribution recovery first revision"}),
        ("sentence", "source", {"kind": "sentence", "text": "Distribution recovery second revision"}),
        ("sentence", "target", {"kind": "sentence", "text": "Distribution linked target"}),
        ("scope", "scope", {"kind": "project", "name": "Owned scope"}),
        ("membership", "scope-source", {"scope": {"kind": "project", "id": "scope"}, "context": {"kind": "sentence", "id": "source"}, "present": True}),
    ]:
        path = "/contexts/workspaces/" + workspace + "/" + kind + "/" + identifier
        version = records.get(path, {}).get("data", {}).get("version", "0")
        records[path] = member.json("POST", path, mutation(version, value=value))
        historical.append(path + "?revision=" + records[path]["data"]["version"])
    ref = lambda name: {"kind": "sentence", "id": name, "revision": records["/contexts/workspaces/" + workspace + "/sentence/" + name]["data"]["version"]}
    relation = {"kind": "reference", "from": ref("source"), "to": ref("target"), "present": True}
    member.json("PUT", "/graph/workspaces/" + workspace + "/relations/owned-reference", mutation(value=relation))
    run_path = "/runs/workspaces/" + workspace
    member.json("POST", run_path + "/executions/owned-unstarted/create",
                mutation(spec={"name": "Saved work without execution", "input": {"command": {"kind": "shell", "target": "owned-no-execution"}}}))
    historical.append(run_path + "/records/run:owned-unstarted")
    # Do not retain bearer/session/verification values in the evidence output.
    fixture = {"records": {path: member.json("GET", path) for path in [*records, *historical]},
               "identities": identities, "workspace": workspace,
               "projections": {"search": {"text": "Distribution", "limit": 20},
                               "graph": {"root": ref("source"), "direction": "both", "depth": 3, "nodes": 100, "edges": 100, "limit": 100}}}
    for index, browser in enumerate(browsers):
        browser.jar.save(str(state / ("acceptance-cookies-" + str(index))), ignore_discard=True)
    (state / "acceptance.json").write_text(json.dumps(fixture))
    (state / "acceptance.json").chmod(0o600)
    verify(state)
    print("PASS: packaged bootstrap, self-registration, local mail verification, two BFF logins, Workspace invite, versions, membership and relation")
finally:
    run(["docker", "stop", mail_name])
    run(["docker", "rm", mail_name])
