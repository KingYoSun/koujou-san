export type Reference = {
  workspace_id: string;
  id: string;
  version: string;
};

export type ResponseMeta = {
  workspace_id: string;
  operation_id?: string;
  position: string | null;
  next_cursor: string | null;
  completeness:
    { state: "complete" } | { state: "partial" | "pending"; reason: string };
};

export type Mutation = {
  operation_id: string;
  expected_version: string;
};

export type Problem = {
  code:
    | "invalid_request"
    | "unauthenticated"
    | "forbidden"
    | "version_conflict"
    | "operation_conflict"
    | "unavailable";
  current_version?: string;
};

export type ApiResponse<T> =
  | { data: T; meta: ResponseMeta; error?: never }
  | { error: Problem; meta: ResponseMeta; data?: never };

function object(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

const text = (value: unknown): value is string =>
  typeof value === "string" && value.length > 0;

export function isReference(value: unknown): value is Reference {
  return (
    object(value) &&
    text(value.workspace_id) &&
    text(value.id) &&
    text(value.version)
  );
}

export function decodeResponse<T>(
  value: unknown,
  dataType: (data: unknown) => data is T,
  workspaceID: string,
): ApiResponse<T> {
  if (!object(value) || !object(value.meta))
    throw new Error("Invalid API response");
  const meta = value.meta;
  if (
    !text(meta.workspace_id) ||
    meta.workspace_id !== workspaceID ||
    (meta.operation_id !== undefined && !text(meta.operation_id)) ||
    !(meta.position === null || text(meta.position)) ||
    !(meta.next_cursor === null || text(meta.next_cursor)) ||
    !object(meta.completeness)
  )
    throw new Error("Invalid response metadata");
  const completeness = meta.completeness;
  if (completeness.state === "complete" && meta.position === null)
    throw new Error("Complete response has no confirmed position");
  if (completeness.state === "complete" && completeness.reason !== undefined)
    throw new Error("Complete response has an incompleteness reason");
  if (
    completeness.state !== "complete" &&
    !(
      (completeness.state === "partial" || completeness.state === "pending") &&
      text(completeness.reason)
    )
  )
    throw new Error("Invalid completeness");
  if ("data" in value === "error" in value)
    throw new Error("Ambiguous API result");
  if ("data" in value) {
    if (!dataType(value.data)) throw new Error("Invalid API data");
    if (
      object(value.data) &&
      "workspace_id" in value.data &&
      value.data.workspace_id !== workspaceID
    )
      throw new Error("Data belongs to another Workspace");
  } else {
    const problem = value.error;
    const codes: string[] = [
      "invalid_request",
      "unauthenticated",
      "forbidden",
      "version_conflict",
      "operation_conflict",
      "unavailable",
    ];
    if (
      !object(problem) ||
      typeof problem.code !== "string" ||
      !codes.includes(problem.code)
    )
      throw new Error("Invalid API problem");
    if (problem.code === "version_conflict" && !text(problem.current_version))
      throw new Error("Missing current version");
    if (problem.current_version !== undefined && !text(problem.current_version))
      throw new Error("Invalid current version");
  }
  return value as ApiResponse<T>;
}
