export class SyncHTTPError extends Error {
  constructor(
    readonly status: number,
    readonly response: unknown,
  ) {
    super(
      status === 409
        ? "version_conflict"
        : status === 403
          ? "forbidden"
          : status === 401
            ? "authentication_required"
            : status === 400
              ? "invalid_request"
              : "api_unavailable",
    );
  }
}
