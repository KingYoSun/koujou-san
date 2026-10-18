import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, realpathSync } from "node:fs";
import {
  copyFile,
  cp,
  mkdir,
  readFile,
  readdir,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { notices } from "./distribution-notices.mjs";

const root = realpathSync(
  path.resolve(path.dirname(fileURLToPath(import.meta.url)), ".."),
);
const version = JSON.parse(
  await readFile(path.join(root, "package.json"), "utf8"),
).version;
const kind = process.argv[2];
const exe = process.platform === "win32" ? ".exe" : "";
const pnpmArgs = (args) => {
  if (!process.env.npm_execpath)
    throw Error("Run pnpm run dist:server or dist:desktop");
  return [process.env.npm_execpath, ...args];
};
const go = path.join(root, ".tools/go/bin", `go${exe}`);
async function run(
  command,
  args,
  { cwd = root, env = process.env, capture = false } = {},
) {
  return await new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env,
      stdio: capture ? ["ignore", "pipe", "inherit"] : "inherit",
      windowsHide: true,
    });
    let output = "";
    if (capture) child.stdout.on("data", (v) => (output += v));
    child.on("error", reject);
    child.on("close", (code) =>
      code === 0
        ? resolve(output.trim())
        : reject(Error(`${path.basename(command)} failed (${code})`)),
    );
  });
}
const commit = await run("git", ["rev-parse", "HEAD"], { capture: true });
if (
  await run("git", ["status", "--porcelain", "--untracked-files=normal"], {
    capture: true,
  })
)
  throw Error("Commit the complete candidate before building a distribution");
const output = path.join(root, "artifacts");
const stage = path.join(output, `${kind}-${version}-${commit.slice(0, 8)}`);
await mkdir(stage, { recursive: true });
for (const name of ["LICENSE", "NOTICE", "CONTRIBUTING.md", "DCO"])
  await copyFile(path.join(root, name), path.join(stage, name));
const web = path.join(stage, "web");
await run(
  process.execPath,
  pnpmArgs(["--filter", "@koujou-san/client", "check"]),
);
await run(
  process.execPath,
  pnpmArgs([
    "--filter",
    "@koujou-san/client",
    "exec",
    "vite",
    "build",
    "--outDir",
    web,
  ]),
);
await notices(root, stage, kind === "desktop" ? "windows" : "linux");
const source = path.join(stage, "source", "koujou-san");
await mkdir(source, { recursive: true });
await run("git", [
  "archive",
  "--format=tar",
  "--output",
  path.join(stage, "source.tar"),
  "HEAD",
]);
await run("tar", ["-xf", path.join(stage, "source.tar"), "-C", source]);
await run("tar", [
  "-czf",
  path.join(stage, "sources.tar.gz"),
  "-C",
  stage,
  "source",
]);
if (kind === "server") {
  const image = `koujou-san/server:${version}-${commit.slice(0, 8)}`;
  const imageDir = path.join(stage, "image");
  await mkdir(imageDir, { recursive: true });
  const env = {
    ...process.env,
    GOOS: "linux",
    GOARCH: "amd64",
    CGO_ENABLED: "0",
    GOTOOLCHAIN: "local",
  };
  for (const binary of ["api", "edge", "auth-bootstrap", "koujou"]) {
    await run(
      go,
      [
        "build",
        "-trimpath",
        "-o",
        path.join(binary === "koujou" ? stage : imageDir, binary),
        `./cmd/${binary}`,
      ],
      { cwd: path.join(root, "apps/server"), env },
    );
  }
  await cp(web, path.join(imageDir, "web"), {
    recursive: true,
  });
  for (const name of [
    "LICENSE",
    "NOTICE",
    "THIRD_PARTY_NOTICES.json",
    "THIRD_PARTY_NOTICES.md",
  ])
    await copyFile(path.join(stage, name), path.join(imageDir, name));
  await cp(path.join(stage, "licenses"), path.join(imageDir, "licenses"), {
    recursive: true,
  });
  await writeFile(
    path.join(imageDir, "Dockerfile"),
    `FROM scratch\nCOPY api edge auth-bootstrap /app/\nCOPY web /app/web/\nCOPY LICENSE NOTICE THIRD_PARTY_NOTICES.json THIRD_PARTY_NOTICES.md /app/\nCOPY licenses /app/licenses/\nLABEL org.opencontainers.image.version="${version}" org.opencontainers.image.revision="${commit}" org.opencontainers.image.licenses="Apache-2.0"\nENTRYPOINT ["/app/api"]\n`,
  );
  await run("docker", [
    "build",
    "--platform",
    "linux/amd64",
    "-t",
    image,
    imageDir,
  ]);
  const imageID = await run(
    "docker",
    ["image", "inspect", "--format", "{{.Id}}", image],
    { capture: true },
  );
  await run("docker", [
    "save",
    "-o",
    path.join(stage, "server-image.tar"),
    image,
  ]);
  await mkdir(path.join(stage, "templates"), { recursive: true });
  for (const [src, dst] of [
    ["infra/ory/identity.schema.json", "identity.schema.json"],
    ["infra/postgres/init.sh", "postgres-init.sh"],
    ["infra/bifrost/config.template.json", "bifrost.json"],
  ])
    await copyFile(path.join(root, src), path.join(stage, "templates", dst));
  await copyFile(
    path.join(root, "docs/runbooks/distribution.md"),
    path.join(stage, "OPERATIONS.md"),
  );
  const schema = Math.max(
    ...(await readdir(path.join(root, "apps/server/internal/persistence")))
      .filter((n) => /^\d+.*\.sql$/.test(n))
      .map((n) => Number(n.slice(0, 3))),
  );
  const files = {};
  for (const name of [
    "server-image.tar",
    "koujou",
    "sources.tar.gz",
    "OPERATIONS.md",
    "templates/identity.schema.json",
    "templates/postgres-init.sh",
    "templates/bifrost.json",
    "THIRD_PARTY_NOTICES.json",
    "COMPONENTS.json",
  ])
    files[name] = createHash("sha256")
      .update(await readFile(path.join(stage, name)))
      .digest("hex");
  await writeFile(
    path.join(stage, "release.json"),
    JSON.stringify(
      { version, commit, schema, image, image_id: imageID, files },
      null,
      2,
    ) + "\n",
  );
  await run("tar", [
    "-czf",
    path.join(output, `koujou-san-server-linux-amd64-${version}.tar.gz`),
    "-C",
    stage,
    "koujou",
    "server-image.tar",
    "release.json",
    "templates",
    "LICENSE",
    "NOTICE",
    "THIRD_PARTY_NOTICES.json",
    "THIRD_PARTY_NOTICES.md",
    "COMPONENTS.json",
    "licenses",
    "sources.tar.gz",
    "OPERATIONS.md",
  ]);
} else if (kind === "desktop") {
  if (process.platform !== "win32")
    throw Error("Build the Windows installer on Windows");
  const config = path.join(stage, "tauri.json");
  await writeFile(
    config,
    JSON.stringify(
      {
        build: {
          frontendDist: path
            .relative(path.join(root, "apps/client/src-tauri"), web)
            .replaceAll("\\", "/"),
          beforeBuildCommand: "",
        },
        bundle: {
          active: true,
          targets: ["nsis"],
          resources: {
            [path.join(stage, "sources.tar.gz")]: "sources.tar.gz",
            [path.join(stage, "licenses")]: "licenses/",
            [path.join(stage, "LICENSE")]: "LICENSE",
            [path.join(stage, "NOTICE")]: "NOTICE",
            [path.join(stage, "THIRD_PARTY_NOTICES.md")]:
              "THIRD_PARTY_NOTICES.md",
            [path.join(stage, "THIRD_PARTY_NOTICES.json")]:
              "THIRD_PARTY_NOTICES.json",
          },
          windows: {
            nsis: {
              installMode: "currentUser",
              installerHooks: path.join(
                root,
                "apps/client/src-tauri/windows/hooks.nsh",
              ),
              displayLanguageSelector: false,
              languages: ["Japanese", "English"],
            },
          },
        },
      },
      null,
      2,
    ),
  );
  await run(
    process.execPath,
    pnpmArgs([
      "--filter",
      "@koujou-san/client",
      "exec",
      "tauri",
      "build",
      "--features",
      "custom-protocol",
      "--config",
      config,
    ]),
  );
  const binaries = path.join(stage, "binaries");
  await mkdir(binaries, { recursive: true });
  await copyFile(
    path.join(root, "target/release/koujou-san-runner.exe"),
    path.join(binaries, "koujou-san-runner.exe"),
  );
  const nsis = path.join(root, "target/release/bundle/nsis");
  for (const name of await readdir(nsis))
    if (name.endsWith(".exe"))
      await copyFile(path.join(nsis, name), path.join(stage, name));
} else throw Error("Use server or desktop");
console.log(`Distribution: ${stage}`);
