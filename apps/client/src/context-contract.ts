export type ContextKind = "sentence" | "paragraph" | "section" | "document";
export type ScopeKind = "product" | "project" | "milestone" | "issue";
export type ContextIdentity<K extends string = ContextKind> = {
  kind: K;
  id: string;
};
export type ContextRef<K extends string = ContextKind> = ContextIdentity<K> & {
  revision: string;
};
export type ContextOrigin = {
  kind: string;
  id: string;
  revision: string;
  digest: string;
  path?: string;
  operation_id?: string;
};
export type FixedContextSource = {
  uri: string;
  revision: string;
  range: string;
  digest: string;
  delivery: "reference" | "snapshot";
  state: "ready" | "unfetched" | "failed";
  structure?: "sentence" | "unstructured";
  snapshot?: unknown;
};
export type ContextUnit = {
  kind: ContextKind;
  tags?: string[];
  name?: string;
  text?: string;
  title?: string;
  children?: ContextRef[];
  attachments?: FixedContextSource[];
  external?: FixedContextSource;
  origin?: ContextOrigin;
};
export type ContextScope = {
  kind: ScopeKind;
  name: string;
  parent?: ContextIdentity<ScopeKind>;
};
export type ContextMembership = {
  scope: ContextIdentity<ScopeKind>;
  context: ContextIdentity;
  present: boolean;
};
export type DirectContext = {
  value?: unknown;
  origin?: ContextOrigin;
  state?: string;
};
export type ContextSelection = {
  mode?: "none" | "select";
  include?: ContextRef[];
  exclude?: ContextRef[];
  direct?: DirectContext[];
  external?: FixedContextSource[];
};
export type ContextBaseline = {
  contexts: ContextRef[];
  memberships: ContextRef<"membership">[];
  inputs?: { run_id: string; selection: ContextSelection }[];
  previous?: ContextRef<"baseline">;
  changed?: ContextIdentity<string>[];
  affected_run_ids?: string[];
};
export type CandidateEvidence = {
  engine: string;
  position: string;
  required_position: string;
  state: "complete" | "partial" | "pending";
  reason?: string;
};
export type ContextResolveRequest = {
  run_id: string;
  baseline?: ContextRef<"baseline">;
  selection: ContextSelection;
  evidence?: CandidateEvidence[];
};
export type ContextPlacement = {
  ref: ContextRef;
  path: number[];
  excerpt?: boolean;
};
export type ContextManifest = {
  schema: "context-manifest-v1";
  assembler: "structured-json-v1";
  workspace_id: string;
  run_id: string;
  baseline?: ContextRef<"baseline">;
  memberships?: ContextRef<"membership">[];
  selection: ContextSelection;
  operation_id: string;
  adopted_by: string;
  state: "none" | "ready";
  input_utf8?: string;
  digest: string;
  placements: ContextPlacement[];
  excluded: ContextRef[];
  duplicates: ContextRef[];
  read_status: "unobserved";
};
export type ContextResolution = {
  ready: boolean;
  missing: { code: string; ref?: ContextRef<string>; source?: string }[];
  manifest?: ContextManifest;
};
export type ContextAddition = {
  manifest: ContextRef<"manifest">;
  pause_id: string;
  response_id: string;
  context: DirectContext;
  read_status: "unobserved";
};
export type ContextRecord<T> = { ref: ContextRef<string>; value: T };

const levels = ["sentence", "paragraph", "section", "document"];
function assert(condition: unknown): asserts condition {
  if (!condition) throw new Error("Invalid Context contract");
}
function object(value: unknown): Record<string, unknown> {
  assert(value !== null && typeof value === "object" && !Array.isArray(value));
  return value as Record<string, unknown>;
}
function reference(value: unknown, unit = true) {
  const r = object(value);
  assert(typeof r.kind === "string" && (!unit || levels.includes(r.kind)));
  assert(typeof r.id === "string" && r.id.length > 0);
  assert(
    typeof r.revision === "string" &&
      r.revision.length > 0 &&
      r.revision !== "head",
  );
}
function inputNode(value: unknown, parentLevel = 5) {
  const node = object(value);
  const level = levels.indexOf(String(node.kind)) + 1;
  assert(level > 0 && level < parentLevel);
  if (level === 1) assert(typeof node.text === "string");
  if (node.children !== undefined) {
    assert(Array.isArray(node.children) && level > 1);
    for (const child of node.children) {
      assert(levels.indexOf(String(object(child).kind)) + 1 === level - 1);
      inputNode(child, level);
    }
  }
}
/** Decode the shared domain result without changing the exact input string. */
export function decodeContextResolution(
  raw: unknown,
  workspace: string,
): ContextResolution {
  const result = object(raw);
  assert(typeof result.ready === "boolean" && Array.isArray(result.missing));
  for (const item of result.missing) {
    const gap = object(item);
    assert(typeof gap.code === "string");
    if (gap.ref !== undefined) reference(gap.ref, false);
  }
  if (!result.ready) {
    assert(result.missing.length > 0 && result.manifest === undefined);
    return result as ContextResolution;
  }
  assert(result.missing.length === 0);
  const m = object(result.manifest);
  assert(
    m.schema === "context-manifest-v1" &&
      m.assembler === "structured-json-v1" &&
      m.workspace_id === workspace,
  );
  assert(
    m.read_status === "unobserved" &&
      typeof m.digest === "string" &&
      /^[0-9a-f]{64}$/.test(m.digest),
  );
  assert(
    typeof m.run_id === "string" &&
      typeof m.operation_id === "string" &&
      typeof m.adopted_by === "string",
  );
  const selection = object(m.selection);
  assert(
    selection.mode === undefined ||
      selection.mode === "none" ||
      selection.mode === "select",
  );
  for (const field of ["include", "exclude"])
    if (selection[field] !== undefined) {
      assert(Array.isArray(selection[field]));
      for (const r of selection[field]) reference(r);
    }
  if (m.baseline !== undefined) {
    reference(m.baseline, false);
    assert(object(m.baseline).kind === "baseline");
  }
  if (m.memberships !== undefined) {
    assert(Array.isArray(m.memberships));
    for (const r of m.memberships) {
      reference(r, false);
      assert(object(r).kind === "membership");
    }
  }
  for (const field of ["placements", "excluded", "duplicates"])
    assert(Array.isArray(m[field]));
  for (const p of m.placements as unknown[]) {
    const placement = object(p);
    reference(placement.ref);
    assert(
      Array.isArray(placement.path) &&
        placement.path.every((n) => Number.isInteger(n) && n >= 0),
    );
  }
  for (const r of [
    ...(m.excluded as unknown[]),
    ...(m.duplicates as unknown[]),
  ])
    reference(r);
  if (m.state === "none")
    assert(
      m.input_utf8 === undefined && (m.placements as unknown[]).length === 0,
    );
  else {
    assert(m.state === "ready" && typeof m.input_utf8 === "string");
    const input = object(JSON.parse(m.input_utf8));
    assert(Array.isArray(input.contexts));
    for (const node of input.contexts) inputNode(node);
  }
  return result as ContextResolution;
}

/** Verify received input bytes; parsing/re-serializing JSON could change them. */
export async function verifyContextInput(
  manifest: ContextManifest,
): Promise<boolean> {
  const bytes = new TextEncoder().encode(manifest.input_utf8 ?? "");
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return (
    Array.from(new Uint8Array(digest), (b) =>
      b.toString(16).padStart(2, "0"),
    ).join("") === manifest.digest
  );
}
