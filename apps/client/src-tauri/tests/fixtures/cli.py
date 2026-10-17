"""Owned subprocess fixture. This is not a product executor or a CLI SDK."""
import base64
import json
import os
from pathlib import Path
import sys
import time

for stream in (sys.stdin, sys.stdout, sys.stderr):
    if hasattr(stream, "reconfigure"):
        stream.reconfigure(encoding="utf-8")

mode = sys.argv[1] if len(sys.argv) > 1 else "plain"
if mode == "--version":
    print("fixture-v1")
    raise SystemExit(0)


def event(value):
    print(json.dumps({"koujou_event": value}, ensure_ascii=False), flush=True)


if "KOUJOU_FIXTURE_SECRET" in os.environ:
    Path("local-secret-observed.txt").write_text(
        os.environ["KOUJOU_FIXTURE_SECRET"], encoding="utf-8"
    )

if mode == "reconnect":
    initial = json.loads(sys.stdin.readline())
    Path("launches.jsonl").open("a", encoding="utf-8").write(json.dumps({"pid": os.getpid(), "input": initial}) + "\n")
    for number in range(10):
        Path("heartbeat.json").write_text(json.dumps({"pid": os.getpid(), "number": number}), encoding="utf-8")
        print(json.dumps({"number": number, "pid": os.getpid()}), flush=True)
        time.sleep(1)
    print("RECONNECT_STDERR_ORIGINAL", file=sys.stderr, flush=True)
elif mode == "sleep":
    initial = json.loads(sys.stdin.readline())
    with Path("launches.jsonl").open("a", encoding="utf-8") as launches:
        launches.write(json.dumps({"pid": os.getpid(), "input": initial}) + "\n")
    for number in range(1800):
        if Path("finish-sleep").exists():
            break
        observation = {"pid": os.getpid(), "number": number, "at": time.time()}
        Path("heartbeat.json").write_text(json.dumps(observation), encoding="utf-8")
        print(json.dumps(observation), flush=True)
        time.sleep(1)
    print("SLEEP_STDERR_ORIGINAL", file=sys.stderr, flush=True)
elif mode == "orphan":
    Path("external-effect.txt").write_text("existing effect survives owner loss", encoding="utf-8")
    tick = 0
    while not Path("finish-orphan").exists():
        Path("heartbeat.json").write_text(json.dumps({"pid": os.getpid(), "tick": tick}), encoding="utf-8")
        tick += 1
        time.sleep(0.2)
elif mode == "dialogue":
    initial = json.loads(sys.stdin.readline())
    print(json.dumps({"received_input": initial["input"]}, ensure_ascii=False), flush=True)
    answers = []
    for name in ("first", "second"):
        event({"type": "pause", "pause": {"id": name, "reason": "固定の質問", "request": {"question": name}}})
        answer = json.loads(sys.stdin.readline())
        assert answer["type"] == "answer" and answer["pause_id"] == name
        answers.append(answer["value"])
        event({"type": "resumed", "pause_id": name, "response_id": answer["response_id"]})
    event({"type": "result", "report": {"decision": "PASS", "answers": answers}})
    print("DIALOGUE_COMPLETED", flush=True)
    print("DIALOGUE_STDERR_ORIGINAL", file=sys.stderr, flush=True)
elif mode in ("stop", "no-stop"):
    Path("external-effect.txt").write_text("created before cancellation", encoding="utf-8")
    print("WAITING_WITH_EXISTING_EFFECT", flush=True)
    if mode == "stop":
        while True:
            time.sleep(0.1)
    else:
        time.sleep(12)
        print("NON_STOPPABLE_NORMAL_COMPLETION", flush=True)
elif mode == "repository":
    Path("run-effect.txt").write_text("owned Run effect", encoding="utf-8")
    print("REPOSITORY_EFFECT_WRITTEN", flush=True)
elif mode == "echo":
    arguments = sys.argv[2:]
    context = b""
    prompt = ""
    if "--context-file" in arguments:
        context = Path(arguments[arguments.index("--context-file") + 1]).read_bytes()
    if "--prompt" in arguments:
        prompt = arguments[arguments.index("--prompt") + 1]
    print(json.dumps({"stdin_b64": base64.b64encode(sys.stdin.buffer.read()).decode(), "context_b64": base64.b64encode(context).decode(), "prompt": prompt}, ensure_ascii=False), flush=True)
else:
    raw = sys.stdin.buffer.read()
    print("Should this ordinary question become a Pause?", flush=True)
    print(json.dumps({"input_b64": base64.b64encode(raw).decode(), "server_secret_visible": "KOUJOU_FIXTURE_SERVER_SECRET" in os.environ}), flush=True)
    print("PLAIN_STDERR_ORIGINAL", file=sys.stderr, flush=True)
