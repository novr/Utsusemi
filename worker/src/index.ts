import type { Env, JITRequest, Target } from "./types";
import {
  authenticate,
  authorizeTarget,
  issueHostJWT,
} from "./auth";
import {
  createInstallationToken,
  createJIT,
  deleteRunner,
  findAppInstallation,
  listRunners,
  parseTarget,
} from "./github";
import { HttpError, toErrorResponse } from "./http";

const CREDENTIAL_EXCHANGE_PATH = "/v1/credentials/exchange";

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    try {
      return await route(request, env);
    } catch (err) {
      return toErrorResponse(err);
    }
  },
} satisfies ExportedHandler<Env>;

async function route(request: Request, env: Env): Promise<Response> {
  const url = new URL(request.url);
  if (request.method === "POST" && url.pathname === "/v1/jitconfig") {
    return handleJIT(request, env);
  }
  if (request.method === "DELETE" && url.pathname.startsWith("/v1/runners/")) {
    return handleDelete(request, env, url);
  }
  if (request.method === "POST" && url.pathname === "/v1/runners/list") {
    return handleListRunners(request, env);
  }
  if (request.method === "POST" && url.pathname === CREDENTIAL_EXCHANGE_PATH) {
    return handleCredentialExchange(request, env);
  }
  return new Response("not found", { status: 404 });
}

async function readJSON<T>(request: Request): Promise<T> {
  const text = await request.text();
  if (!text) {
    throw new HttpError(400, "invalid json");
  }
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new HttpError(400, "invalid json");
  }
}

type TargetBody = { target: Target };

async function withBrokerAuth<TBody extends TargetBody, TResult>(
  request: Request,
  env: Env,
  handler: (ctx: {
    body: TBody;
    target: Target;
    installationToken: string;
  }) => Promise<TResult>,
): Promise<TResult> {
  const credential = await authenticate(env, request.headers.get("Authorization"));
  const body = await readJSON<TBody>(request);
  const target = parseTarget(body.target);
  authorizeTarget(credential, target);
  const installationToken = await createInstallationToken(env, credential.installationId);
  return handler({ body, target, installationToken });
}

async function handleJIT(request: Request, env: Env): Promise<Response> {
  const jit = await withBrokerAuth(request, env, async ({ body, target, installationToken }) => {
    const req = body as JITRequest;
    return createJIT(installationToken, target, req.labels, req.name);
  });
  return Response.json({
    encoded_jit_config: jit.encoded_jit_config,
    runner: jit.runner,
  });
}

async function handleDelete(
  request: Request,
  env: Env,
  url: URL,
): Promise<Response> {
  const runnerId = Number(url.pathname.split("/").pop());
  if (!Number.isInteger(runnerId) || runnerId <= 0) {
    throw new HttpError(400, "bad request");
  }
  await withBrokerAuth(request, env, async ({ target, installationToken }) => {
    await deleteRunner(installationToken, target, runnerId);
  });
  return new Response(null, { status: 204 });
}

async function handleListRunners(request: Request, env: Env): Promise<Response> {
  const runners = await withBrokerAuth(request, env, async ({ body, target, installationToken }) => {
    const req = body as { target: Target; prefix?: string };
    return listRunners(installationToken, target, req.prefix ?? "");
  });
  return Response.json({ runners });
}

async function handleCredentialExchange(request: Request, env: Env): Promise<Response> {
  const userToken = request.headers.get("Authorization")?.replace(/^Bearer\s+/i, "");
  if (!userToken) {
    throw new HttpError(401, "unauthorized");
  }
  const body = await readJSON<{ target: Target }>(request);
  const target = parseTarget(body.target);

  const installationId = await findAppInstallation(env, userToken, target.org);
  if (installationId === null) {
    throw new HttpError(404, "installation not found");
  }
  const credential = await issueHostJWT(env, installationId, target);
  return Response.json({ credential, target });
}
