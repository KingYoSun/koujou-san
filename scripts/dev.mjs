import { spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const versions = JSON.parse(
  await readFile(path.join(root, "scripts/toolchains.json"), "utf8"),
);
const task = process.argv[2];
const serverDir = path.join(root, "apps/server");
const goDir = path.join(root, ".tools/go");
const exe = process.platform === "win32" ? ".exe" : "";
let go = path.join(goDir, `bin/go${exe}`);
const env = { ...process.env, GOTOOLCHAIN: "local" };
if (existsSync(path.join(root, ".env"))) {
  for (const line of (await readFile(path.join(root, ".env"), "utf8")).split(
    /\r?\n/,
  )) {
    const match = line.match(/^([A-Z_]+)=(.*)$/);
    if (match && env[match[1]] === undefined) env[match[1]] = match[2];
  }
}

function run(
  command,
  args = [],
  { cwd = root, capture = false, optional = false } = {},
) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env,
      stdio: capture ? ["ignore", "pipe", "pipe"] : "inherit",
    });
    let output = "";
    if (capture) {
      child.stdout.on("data", (chunk) => {
        output += chunk;
      });
      child.stderr.on("data", (chunk) => {
        output += chunk;
      });
    }
    child.on("error", (error) => (optional ? resolve(null) : reject(error)));
    child.on("exit", (code) => {
      if (code === 0) resolve(output.trim());
      else if (optional) resolve(null);
      else
        reject(
          new Error(
            `${path.basename(command)} ${args.join(" ")} failed (${code})${output ? `\n${output}` : ""}`,
          ),
        );
    });
  });
}

function pnpm(args) {
  if (!process.env.npm_execpath)
    throw new Error("Run this task with pnpm (for example: pnpm run setup).");
  return run(process.execPath, [process.env.npm_execpath, ...args]);
}
const client = (command) => pnpm(["--filter", "@koujou-san/client", command]);
const cargo = (command, args = []) =>
  run(`cargo${exe}`, [command, "--locked", ...args]);
const goRun = (args, options = {}) =>
  run(go, args, { cwd: serverDir, ...options });

async function resolveGo(install = false) {
  const expected = `go${versions.go} `;
  for (const candidate of [go, `go${exe}`]) {
    const version = await run(candidate, ["version"], {
      capture: true,
      optional: true,
    });
    if (version?.includes(expected)) {
      go = candidate;
      return;
    }
  }
  if (!install)
    throw new Error(`Go ${versions.go} is missing. Run pnpm run setup.`);
  const archive = versions.goArchives[`${process.platform}-${process.arch}`];
  if (!archive)
    throw new Error(
      `No Go archive configured for ${process.platform}/${process.arch}.`,
    );
  if (existsSync(goDir))
    throw new Error(
      "An incompatible .tools/go exists. Move it aside before rerunning setup.",
    );
  console.log(
    `Installing repository-local Go ${versions.go} from go.dev (SHA-256 verified).`,
  );
  const response = await fetch(`https://go.dev/dl/${archive.file}`);
  if (!response.ok)
    throw new Error(`Go download failed: HTTP ${response.status}`);
  const bytes = Buffer.from(await response.arrayBuffer());
  if (createHash("sha256").update(bytes).digest("hex") !== archive.sha256)
    throw new Error("Go archive checksum mismatch.");
  const toolsDir = path.join(root, ".tools");
  await mkdir(toolsDir, { recursive: true });
  const archivePath = path.join(toolsDir, archive.file);
  await writeFile(archivePath, bytes);
  await run(`tar${exe}`, [
    process.platform === "win32" ? "-xf" : "-xzf",
    archivePath,
    "-C",
    toolsDir,
  ]);
  await resolveGo();
}

async function setup() {
  checkNode();
  const pnpmVersion = await run(
    process.execPath,
    [process.env.npm_execpath, "--version"],
    { capture: true },
  );
  if (pnpmVersion !== versions.pnpm)
    throw new Error(`Use pnpm ${versions.pnpm}; found ${pnpmVersion}.`);
  await resolveGo(true);
  if (!existsSync(path.join(root, ".env"))) {
    const example = await readFile(path.join(root, ".env.example"), "utf8");
    const generated = example.replace(
      /=__GENERATE_SECRET__/g,
      () => `=${randomBytes(24).toString("hex").slice(0, 32)}`,
    );
    await writeFile(path.join(root, ".env"), generated, {
      flag: "wx",
      mode: 0o600,
    });
    console.log("Created local .env with generated development credentials.");
  }
  await pnpm(["install", "--frozen-lockfile"]);
  await run(`rustup${exe}`, [
    "toolchain",
    "install",
    versions.rust,
    "--profile",
    "minimal",
    "--no-self-update",
    "--component",
    "rustfmt",
    "--component",
    "clippy",
  ]);
  await cargo("fetch");
  await goRun(["mod", "download"]);
  console.log(
    "Setup complete. Run pnpm run doctor, then pnpm run check / test / build.",
  );
}

function checkNode() {
  const [major, minor] = process.versions.node.split(".").map(Number);
  if (major !== versions.nodeMajor || minor < 14) {
    throw new Error(
      `Use Node ${versions.nodeMajor}.x >= ${versions.nodeMinimum}; found ${process.versions.node}.`,
    );
  }
}

async function doctor() {
  checkNode();
  await resolveGo();
  console.log(`Node ${process.versions.node}`);
  for (const [command, args] of [
    [go, ["version"]],
    [`cargo${exe}`, ["--version"]],
    [`rustc${exe}`, ["--version"]],
    [`git${exe}`, ["--version"]],
  ]) {
    console.log(await run(command, args, { capture: true }));
  }
  if (!existsSync(path.join(root, "node_modules")))
    throw new Error("node_modules missing. Run pnpm run setup.");
  const docker = await run(
    `docker${exe}`,
    ["info", "--format", "{{.ServerVersion}}"],
    { capture: true, optional: true },
  );
  console.log(
    docker
      ? `Docker daemon ${docker}`
      : "Docker daemon unavailable (required only for infra tasks).",
  );
  const gh = await run(`gh${exe}`, ["--version"], {
    capture: true,
    optional: true,
  });
  console.log(
    gh?.split("\n")[0] ??
      "gh unavailable (optional; direct user instructions still supported).",
  );
}

async function infra(command) {
  if (!existsSync(path.join(root, ".env")))
    throw new Error("Local .env missing. Run pnpm run setup.");
  const args = [
    "compose",
    "--env-file",
    path.join(root, ".env"),
    "-f",
    path.join(root, "infra/compose.yaml"),
  ];
  const actions = {
    up: ["up", "-d", "--wait", "--wait-timeout", "180"],
    down: ["down"],
    status: ["ps"],
  };
  await run(`docker${exe}`, [...args, ...actions[command]]);
}

const tasks = {
  setup,
  doctor,
  check: async () => {
    await resolveGo();
    const unformatted = await run(
      path.join(path.dirname(go), `gofmt${exe}`),
      ["-l", "."],
      { cwd: serverDir, capture: true },
    );
    if (unformatted) throw new Error(`Run gofmt on:\n${unformatted}`);
    await goRun(["vet", "./..."]);
    await client("check");
    await pnpm(["run", "format:check"]);
    await run(`cargo${exe}`, ["fmt", "--all", "--", "--check"]);
    await run(`git${exe}`, ["diff", "--check"]);
  },
  test: async () => {
    await resolveGo();
    await goRun(["test", "./..."]);
    const fixturePath = path.join(root, "test-results/contracts.json");
    await mkdir(path.dirname(fixturePath), { recursive: true });
    await writeFile(
      fixturePath,
      await goRun(["run", "./cmd/contract-fixtures"], { capture: true }),
    );
    env.KOUJOU_CONTRACT_FIXTURES = fixturePath;
    await client("test");
  },
  build: async () => {
    await resolveGo();
    await mkdir(path.join(root, "dist"), { recursive: true });
    await goRun([
      "build",
      "-o",
      path.join(root, `dist/koujou-api${exe}`),
      "./cmd/api",
    ]);
    await client("build");
  },
  "check:desktop": () => cargo("check", ["--workspace"]),
  "test:desktop": () => cargo("test", ["--workspace"]),
  "build:desktop": () => client("tauri:build"),
  "dev:api": async () => {
    await resolveGo();
    await goRun(["run", "./cmd/api"]);
  },
  "dev:web": () => client("dev"),
  "dev:desktop": () => client("tauri:dev"),
  "infra:up": () => infra("up"),
  "infra:down": () => infra("down"),
  "infra:status": () => infra("status"),
};

const startedAt = new Date().toISOString();
try {
  if (!tasks[task])
    throw new Error(
      `Unknown task: ${task ?? "(missing)"}. Tasks: ${Object.keys(tasks).join(", ")}`,
    );
  await tasks[task]();
  await mkdir(path.join(root, "test-results"), { recursive: true });
  await writeFile(
    path.join(root, `test-results/${task.replaceAll(":", "-")}.json`),
    `${JSON.stringify({ task, status: "pass", startedAt, finishedAt: new Date().toISOString() }, null, 2)}\n`,
  );
} catch (error) {
  process.stderr.write(`${error.message}\n`);
  await mkdir(path.join(root, "test-results"), { recursive: true });
  await writeFile(
    path.join(root, `test-results/${String(task).replaceAll(":", "-")}.json`),
    `${JSON.stringify({ task, status: "fail", startedAt, finishedAt: new Date().toISOString(), error: error.message }, null, 2)}\n`,
  );
  process.exitCode = 1;
}
