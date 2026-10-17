import { spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { existsSync } from "node:fs";
import { appendFile, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { createServer } from "node:net";
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
  await prepareAuth();
  console.log(
    "Setup complete. Run pnpm run doctor, then pnpm run check / test / build.",
  );
}

async function prepareAuth() {
  await resolveGo();
  const additions = [];
  for (const key of ["KOUJOU_AUTH_KEY", "KOUJOU_WEB_CLIENT_SECRET"]) {
    if (!env[key]) {
      env[key] = randomBytes(16).toString("hex");
      additions.push(`${key}=${env[key]}`);
    }
  }
  if (env.KOUJOU_AUTH_ENABLED === undefined) {
    env.KOUJOU_AUTH_ENABLED = "1";
    additions.push("KOUJOU_AUTH_ENABLED=1");
  }
  if (additions.length)
    await appendFile(path.join(root, ".env"), `\n${additions.join("\n")}\n`, {
      mode: 0o600,
    });
  await goRun(["run", "./cmd/devcert", path.join(root, ".cache/tls")]);
  env.KOUJOU_TLS_CERT ??= path.join(root, ".cache/tls/cert.pem");
  env.KOUJOU_TLS_KEY ??= path.join(root, ".cache/tls/key.pem");
  env.KOUJOU_DEV_CA_FILE ??= env.KOUJOU_TLS_CERT;
  env.KOUJOU_DATABASE_URL ??= `postgresql://koujou:${encodeURIComponent(env.KOUJOU_DB_PASSWORD)}@127.0.0.1:15432/koujou?sslmode=disable`;
  env.KOUJOU_GRAPH_URL ??= "http://127.0.0.1:2480";
  env.KOUJOU_GRAPH_USER ??= "root";
  env.KOUJOU_GRAPH_PASSWORD ??= env.ARCADEDB_ROOT_PASSWORD;
  env.KOUJOU_GRAPH_DATABASE ??= "koujou_graph";
  env.KOUJOU_SEARCH_DIR = path.resolve(
    root,
    env.KOUJOU_SEARCH_DIR || ".cache/search",
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

async function testDatabase(suite = "persistence", testName) {
  const authentication = suite === "auth" || suite === "mcpapi";
  const graph = suite === "graphindex" || suite === "mcpapi";
  await resolveGo();
  if (!env.KOUJOU_DB_PASSWORD) throw new Error("Run pnpm run setup first.");
  const project = `koujou-san-db-test-${randomBytes(6).toString("hex")}`;
  const reservation = createServer();
  await new Promise((resolve, reject) => {
    reservation.once("error", reject);
    reservation.listen(0, "127.0.0.1", resolve);
  });
  env.KOUJOU_TEST_PG_PORT = String(reservation.address().port);
  await new Promise((resolve, reject) =>
    reservation.close((error) => (error ? reject(error) : resolve())),
  );
  if (authentication || graph) {
    for (const name of [
      ...(authentication
        ? ["HYDRA", "HYDRA_ADMIN", "KRATOS", "KRATOS_ADMIN", "MAILPIT", "API"]
        : []),
      ...(graph ? ["ARCADE"] : []),
    ]) {
      const server = createServer();
      await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(0, "127.0.0.1", resolve);
      });
      env[`KOUJOU_TEST_${name}_PORT`] = String(server.address().port);
      await new Promise((resolve) => server.close(resolve));
    }
    env.KOUJOU_TEST_SITE = `https://127.0.0.1:${env.KOUJOU_TEST_API_PORT}`;
  }
  const compose = [
    "compose",
    "--env-file",
    path.join(root, ".env"),
    "-p",
    project,
    "-f",
    path.join(root, "infra/compose.yaml"),
    "-f",
    path.join(
      root,
      authentication
        ? "infra/compose.auth-test.yaml"
        : graph
          ? "infra/compose.graph-test.yaml"
          : "infra/compose.test.yaml",
    ),
  ];
  if (authentication && graph)
    compose.push("-f", path.join(root, "infra/compose.graph-test.yaml"));
  console.log(
    "Creating isolated PostgreSQL fixtures; only this test container is stopped and restored.",
  );
  try {
    await run(`docker${exe}`, [
      ...compose,
      "up",
      "-d",
      "--wait",
      "--wait-timeout",
      "180",
      ...(authentication
        ? ["kratos", "hydra", "mailpit", ...(graph ? ["arcadedb"] : [])]
        : graph
          ? ["postgres", "arcadedb"]
          : ["postgres"]),
    ]);
    const container = (
      await run(`docker${exe}`, [...compose, "ps", "-q", "postgres"], {
        capture: true,
      })
    ).trim();
    // initdb's temporary server is followed by a shutdown/restart. Wait for
    // the final PID 1 and TCP listener before opening the test database.
    const deadline = Date.now() + 180_000;
    let databaseReady = false;
    while (Date.now() < deadline) {
      const probe = await run(
        `docker${exe}`,
        [
          "exec",
          container,
          "sh",
          "-c",
          'test "$(cat /proc/1/comm)" = postgres && pg_isready -h 127.0.0.1 -U postgres -d postgres',
        ],
        { capture: true, optional: true },
      );
      if (probe !== null) {
        databaseReady = true;
        break;
      }
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
    if (!databaseReady)
      throw new Error("Test PostgreSQL did not finish initialization.");
    const port = (
      await run(`docker${exe}`, ["port", container, "5432/tcp"], {
        capture: true,
      })
    ).match(/^127\.0\.0\.1:(\d+)$/m)?.[1];
    if (!port)
      throw new Error("Test PostgreSQL loopback port was not published.");
    env.KOUJOU_TEST_DATABASE_URL = `postgresql://koujou:${encodeURIComponent(env.KOUJOU_DB_PASSWORD)}@127.0.0.1:${port}/koujou?sslmode=disable&connect_timeout=2`;
    env.KOUJOU_TEST_CONTAINER = container;
    env.KOUJOU_TEST_PROJECT = project;
    if (graph) {
      env.KOUJOU_TEST_GRAPH_CONTAINER = (
        await run(`docker${exe}`, [...compose, "ps", "-q", "arcadedb"], {
          capture: true,
        })
      ).trim();
      env.KOUJOU_TEST_GRAPH_URL = `http://127.0.0.1:${env.KOUJOU_TEST_ARCADE_PORT}`;
      env.KOUJOU_TEST_GRAPH_PASSWORD = env.ARCADEDB_ROOT_PASSWORD;
    }
    await goRun([
      "test",
      `./internal/${suite}`,
      "-run",
      `^${testName ?? { auth: "TestOryIntegration", access: "TestAccessPostgres", contexts: "TestContextsPostgres", sharedsync: "TestSyncPostgres", searchindex: "TestSearchPostgres", graphindex: "TestGraphPostgres", mcpapi: "TestMCPIntegration", githubobserve: "TestGithubPostgres", persistence: "TestPostgres" }[suite]}$`,
      "-count=1",
      ...(env.KOUJOU_TEST_MCP_UI === "1" ? ["-timeout=35m"] : []),
      "-v",
    ]);
  } finally {
    const owned = await run(
      `docker${exe}`,
      [...compose, "ps", "-a", "-q", "postgres"],
      { capture: true, optional: true },
    );
    if (owned) {
      const label = await run(
        `docker${exe}`,
        [
          "inspect",
          "--format",
          '{{index .Config.Labels "com.docker.compose.project"}}',
          owned.trim(),
        ],
        { capture: true },
      );
      if (label !== project)
        throw new Error(
          "Refusing cleanup of a PostgreSQL container from another project.",
        );
      await run(`docker${exe}`, [...compose, "down", "--volumes"]);
    }
  }
}

const tasks = {
  "test:workbench": () => testDatabase("workbench", "TestWorkbenchPostgres"),
  "test:workbench:ui": async () => {
    env.KOUJOU_TEST_WORKBENCH_UI = "1";
    env.KOUJOU_TEST_MCP_UI = "1";
    await client("build");
    await testDatabase("mcpapi");
  },
  "test:publications": () =>
    testDatabase("publications", "TestPublicationsPostgres"),
  "test:publications:live": () => {
    env.KOUJOU_TEST_PUBLICATION_LIVE = "1";
    return testDatabase("publications", "TestPublicationsLive");
  },
  "test:inspections": () =>
    testDatabase("inspections", "TestInspectionsPostgres"),
  "test:runs": () => testDatabase("runs", "TestRunsPostgres"),
  "test:workflows": () => testDatabase("workflows", "TestWorkflowsPostgres"),
  "test:automation": () => testDatabase("automation", "TestAutomationPostgres"),
  "test:usage": () => testDatabase("usage", "TestUsagePostgres"),
  "test:github": () => testDatabase("githubobserve"),
  "test:github:live": async () => {
    await resolveGo();
    env.KOUJOU_TEST_GITHUB_LIVE = "1";
    await goRun([
      "test",
      "./internal/githubobserve",
      "-run",
      "^TestGithubLive$",
      "-count=1",
      "-v",
    ]);
  },
  "test:github:ui": async () => {
    env.KOUJOU_TEST_GITHUB_LIVE = "1";
    env.KOUJOU_TEST_MCP_UI = "1";
    await client("build");
    await testDatabase("mcpapi");
  },
  "test:graph": () => testDatabase("graphindex"),
  "test:graph-load": () => {
    env.KOUJOU_GRAPH_LOAD_REPORT = path.join(
      root,
      "test-results/graph-load.json",
    );
    return testDatabase("graphindex", "TestGraphLoad");
  },
  "test:search": () => testDatabase("searchindex"),
  "test:search-load": () => {
    env.KOUJOU_SEARCH_LOAD_REPORT = path.join(
      root,
      "test-results/search-load.json",
    );
    return testDatabase("searchindex", "TestSearchLoad");
  },
  setup,
  doctor,
  "test:database": () => testDatabase(),
  "test:auth": () => testDatabase("auth"),
  "test:mcp": () => testDatabase("mcpapi"),
  "test:mcp:ui": async () => {
    await client("build");
    env.KOUJOU_TEST_MCP_UI = "1";
    await testDatabase("mcpapi");
  },
  "test:access": () => testDatabase("access"),
  "test:contexts": () => testDatabase("contexts"),
  "test:sync": () => testDatabase("sharedsync"),
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
    const contextPath = path.join(root, "test-results/contexts.json");
    await writeFile(
      contextPath,
      await goRun(["run", "./cmd/context-fixtures"], { capture: true }),
    );
    env.KOUJOU_CONTEXT_FIXTURES = contextPath;
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
  "build:desktop": async () => {
    await client("tauri:build");
    await cargo("build", [
      "--release",
      "-p",
      "koujou-san-desktop",
      "--bin",
      "koujou-san-runner",
    ]);
  },
  "dev:api": async () => {
    await prepareAuth();
    if (!env.KOUJOU_DATABASE_URL && env.KOUJOU_DB_PASSWORD) {
      env.KOUJOU_DATABASE_URL = `postgresql://koujou:${encodeURIComponent(env.KOUJOU_DB_PASSWORD)}@127.0.0.1:15432/koujou?sslmode=disable`;
    }
    await goRun(["run", "./cmd/api"]);
  },
  "dev:web": async () => {
    await prepareAuth();
    await client("dev");
  },
  "dev:desktop": async () => {
    await prepareAuth();
    await cargo("build", [
      "-p",
      "koujou-san-desktop",
      "--bin",
      "koujou-san-runner",
    ]);
    await client("tauri:dev");
  },
  "auth:bootstrap": async () => {
    await prepareAuth();
    const args = process.argv.slice(3);
    const index = args.indexOf("--password-file");
    if (index >= 0 && args[index + 1])
      args[index + 1] = path.resolve(root, args[index + 1]);
    await goRun(["run", "./cmd/auth-bootstrap", ...args]);
  },
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
