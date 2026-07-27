export class HttpError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "HttpError";
    this.status = status;
  }
}

export function toErrorResponse(err: unknown): Response {
  if (err instanceof HttpError) {
    return new Response(err.message, { status: err.status });
  }
  console.error("unhandled error", err instanceof Error ? err.stack ?? err.message : err);
  return new Response("internal error", { status: 500 });
}
