export type ApiHealth = {
  status: "ok";
  service: "koujou-san-api";
  stage: "bootstrap";
};

export async function fetchHealth(
  apiBaseUrl: string,
  signal?: AbortSignal,
): Promise<ApiHealth> {
  const response = await fetch(`${apiBaseUrl.replace(/\/$/, "")}/healthz`, {
    signal,
  });
  if (!response.ok) {
    throw new Error(`API health returned ${response.status}`);
  }
  const body: unknown = await response.json();
  if (
    typeof body !== "object" ||
    body === null ||
    !("status" in body) ||
    !("service" in body) ||
    !("stage" in body) ||
    body.status !== "ok" ||
    body.service !== "koujou-san-api" ||
    body.stage !== "bootstrap"
  ) {
    throw new Error("API health response is not from the bootstrap API");
  }
  return body as ApiHealth;
}
