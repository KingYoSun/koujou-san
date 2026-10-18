import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  existsSync,
  readFileSync,
  readdirSync,
  realpathSync,
  statSync,
} from "node:fs";
import { cp, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const names =
  /^(licen[cs]e|copying|copyright|notice|patents|unlicense)([.-]|$)/i;
const safe = (s) => s.replace(/[^a-zA-Z0-9_.-]/g, "_");
const run = (command, args, cwd, env = process.env) =>
  execFileSync(command, args, {
    cwd,
    env,
    encoding: "utf8",
    maxBuffer: 64 << 20,
  });
const sequence = (s) =>
  s
    .trim()
    .split(/\r?\n(?=\{)/)
    .map((v) => JSON.parse(v));

function selected(expression) {
  if (expression === "(MIT OR Apache-2.0) AND Unicode-3.0")
    return "MIT AND Unicode-3.0";
  if (expression.includes(" AND ")) return expression;
  for (const candidate of [
    "MIT",
    "Apache-2.0",
    "BSD-3-Clause",
    "Zlib",
    "ISC",
    "0BSD",
  ]) {
    if (expression.split(/\s+OR\s+|\s*\/\s*/).includes(candidate))
      return candidate;
  }
  if (
    [
      "MPL-2.0",
      "Unicode-3.0",
      "Unlicense",
      "Apache-2.0 WITH LLVM-exception",
    ].includes(expression)
  )
    return expression;
  throw Error(
    `Review the license expression before distribution: ${expression}`,
  );
}
function identify(text) {
  if (/Mozilla Public License[\s\S]*Version 2\.0/.test(text)) return "MPL-2.0";
  if (/Apache License[\s\S]*Version 2\.0/.test(text)) return "Apache-2.0";
  if (/Permission is hereby granted, free of charge/i.test(text)) return "MIT";
  if (/Permission to use, copy, modify, and(?:\/or)? distribute/i.test(text))
    return "ISC";
  if (/Redistribution and use in source and binary forms/i.test(text))
    return /neither the name/i.test(text) ? "BSD-3-Clause" : "BSD-2-Clause";
  if (
    /free and unencumbered software released into the public domain/i.test(text)
  )
    return "Unlicense";
  throw Error(
    "Unidentified license text; inspect the fixed dependency before shipping",
  );
}

export async function notices(root, stage, target) {
  const records = [];
  const overrides = JSON.parse(
    await readFile(
      path.join(root, "legal/third-party/crate-licenses.json"),
      "utf8",
    ),
  );
  await mkdir(path.join(stage, "licenses"), { recursive: true });
  await mkdir(path.join(stage, "source", "dependencies"), { recursive: true });
  async function add(kind, name, version, dir, declared) {
    let files = readdirSync(dir).filter(
      (n) => names.test(n) && statSync(path.join(dir, n)).isFile(),
    );
    const sourceDir = dir;
    const override =
      kind === "rust" ? overrides[`${name}@${version}`] : undefined;
    if (override) {
      dir = path.join(root, "legal/third-party");
      files = [override.file];
    }
    if (files.length === 0 && kind === "rust") {
      let parent = path.dirname(dir);
      for (let n = 0; n < 4 && files.length === 0; n++) {
        files = readdirSync(parent).filter(
          (n) => names.test(n) && statSync(path.join(parent, n)).isFile(),
        );
        if (files.length) {
          dir = parent;
          break;
        }
        parent = path.dirname(parent);
      }
    }
    if (!files.length)
      throw Error(`No license text: ${kind}:${name}@${version}`);
    const text = files
      .map((f) => readFileSync(path.join(dir, f), "utf8"))
      .join("\n");
    const license = declared ? selected(declared) : identify(text);
    const key = safe(`${kind}-${name}-${version}`);
    await mkdir(path.join(stage, "licenses", key), { recursive: true });
    for (const file of files)
      await cp(path.join(dir, file), path.join(stage, "licenses", key, file), {
        recursive: true,
      });
    let source;
    if (license.includes("MPL-2.0")) {
      source = `source/dependencies/${key}`;
      await cp(sourceDir, path.join(stage, source), {
        recursive: true,
        dereference: true,
        filter: (f) =>
          !["target", "node_modules", ".git"].includes(path.basename(f)),
      });
    }
    records.push({
      kind,
      name,
      version,
      declared: declared || license,
      selected: license,
      notices: `licenses/${key}`,
      ...(source ? { source } : {}),
    });
  }
  const node = JSON.parse(
    run(
      process.execPath,
      [process.env.npm_execpath, "licenses", "list", "--prod", "--json"],
      root,
    ),
  );
  for (const group of Object.values(node))
    for (const p of group) {
      await add("node", p.name, p.versions.join(","), p.paths[0], p.license);
    }
  const tailwind = realpathSync(
    path.join(root, "apps/client/node_modules/tailwindcss"),
  );
  const tw = JSON.parse(
    await readFile(path.join(tailwind, "package.json"), "utf8"),
  );
  await add("css", tw.name, tw.version, tailwind, tw.license);
  const cli = realpathSync(
    path.join(root, "apps/client/node_modules/@tauri-apps/cli"),
  );
  const cliInfo = JSON.parse(
    await readFile(path.join(cli, "package.json"), "utf8"),
  );
  if (target === "windows")
    await add(
      "installer-tool",
      cliInfo.name,
      cliInfo.version,
      cli,
      cliInfo.license,
    );
  if (target === "windows") {
    const sysroot = run("rustc", ["--print", "sysroot"], root).trim();
    await add(
      "rust-runtime",
      "Rust standard library",
      "1.98.1",
      path.join(sysroot, "share/doc/rust"),
      "MIT OR Apache-2.0",
    );
    const meta = JSON.parse(
      run(
        "cargo",
        [
          "metadata",
          "--features",
          "custom-protocol",
          "--locked",
          "--offline",
          "--format-version",
          "1",
          "--filter-platform",
          "x86_64-pc-windows-msvc",
        ],
        root,
      ),
    );
    const nodes = new Map(meta.resolve.nodes.map((n) => [n.id, n]));
    const active = new Set();
    const visit = (id) => {
      if (active.has(id)) return;
      active.add(id);
      for (const dep of nodes.get(id)?.deps || []) visit(dep.pkg);
    };
    for (const id of meta.workspace_default_members) visit(id);
    for (const p of meta.packages)
      if (p.source && active.has(p.id))
        await add(
          "rust",
          p.name,
          p.version,
          path.dirname(p.manifest_path),
          p.license,
        );
  } else {
    const go = path.join(
      root,
      ".tools/go/bin",
      process.platform === "win32" ? "go.exe" : "go",
    );
    const env = {
      ...process.env,
      GOOS: "linux",
      GOARCH: "amd64",
      CGO_ENABLED: "0",
      GOTOOLCHAIN: "local",
    };
    const pkgs = sequence(
      run(
        go,
        [
          "list",
          "-deps",
          "-json",
          "./cmd/api",
          "./cmd/edge",
          "./cmd/koujou",
          "./cmd/auth-bootstrap",
        ],
        path.join(root, "apps/server"),
        env,
      ),
    );
    const modules = new Map();
    for (const p of pkgs)
      if (p.Module && !p.Module.Main)
        modules.set(p.Module.Path, p.Module.Replace || p.Module);
    for (const m of modules.values()) await add("go", m.Path, m.Version, m.Dir);
    const goroot = run(go, ["env", "GOROOT"], root).trim();
    await add("go", "Go runtime", "1.27.2", goroot, "BSD-3-Clause");
  }
  if (target === "windows") {
    const extra = JSON.parse(
      await readFile(path.join(root, "legal/third-party/index.json"), "utf8"),
    );
    for (const [name, item] of Object.entries(extra)) {
      if (name === "webview2-rs") continue;
      const key = safe(`native-${name}-${item.version}`);
      const dest = path.join(stage, "licenses", key);
      await mkdir(dest, { recursive: true });
      for (const file of item.files)
        await cp(
          path.join(root, "legal/third-party", file),
          path.join(dest, file),
          { recursive: true, dereference: true },
        );
      records.push({
        kind: "native",
        name,
        version: item.version,
        declared: item.license,
        selected: item.license,
        notices: `licenses/${key}`,
        origin: item.source,
      });
    }
  } else {
    const components = JSON.parse(
      await readFile(
        path.join(root, "apps/server/internal/distribution/components.json"),
        "utf8",
      ),
    );
    for (const [name, c] of Object.entries(components)) {
      const response = await fetch(c.license_url, {
        signal: AbortSignal.timeout(20000),
      });
      if (!response.ok) throw Error(`Component license unavailable: ${name}`);
      const data = Buffer.from(await response.arrayBuffer());
      const dir = path.join(stage, "licenses", `component-${name}`);
      await mkdir(dir, { recursive: true });
      await writeFile(path.join(dir, "LICENSE"), data);
      c.license_sha256 = createHash("sha256").update(data).digest("hex");
    }
    await writeFile(
      path.join(stage, "COMPONENTS.json"),
      JSON.stringify(components, null, 2) + "\n",
    );
  }
  await writeFile(
    path.join(stage, "THIRD_PARTY_NOTICES.json"),
    JSON.stringify({ target, dependencies: records }, null, 2) + "\n",
  );
  await writeFile(
    path.join(stage, "THIRD_PARTY_NOTICES.md"),
    "# Third-party notices\n\nOriginal components retain their licenses. The selected licenses and verbatim notices are listed below. MPL-covered sources are provided in the source archive.\n\n" +
      records
        .map(
          (r) =>
            `- ${r.kind} ${r.name} ${r.version}: ${r.selected}; ${r.notices}${r.source ? `; ${r.source}` : ""}`,
        )
        .join("\n") +
      "\n",
  );
}
