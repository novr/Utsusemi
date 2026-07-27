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
  listRunners,
  parseTarget,
} from "./github";

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    try {
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
      if (request.method === "POST" && url.pathname === "/v1/register/exchange") {
        return handleRegisterExchange(request, env);
      }
      return new Response("not found", { status: 404 });
    } catch (err) {
      if (err instanceof Response) return err;
      return new Response("internal error", { status: 500 });
    }
  },
};

async function handleJIT(request: Request, env: Env): Promise<Response> {
  const credential = await authenticate(env, request.headers.get("Authorization"));
  const body = (await request.json()) as JITRequest;
  const target = parseTarget(body.target);
  authorizeTarget(credential, target);

  const token = await createInstallationToken(env, credential.installationId);
  const jit = await createJIT(token, target, body.labels, body.name);
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
  const credential = await authenticate(env, request.headers.get("Authorization"));
  const runnerId = Number(url.pathname.split("/").pop());
  if (!Number.isInteger(runnerId) || runnerId <= 0) {
    return new Response("bad request", { status: 400 });
  }
  const body = (await request.json()) as { target: Target };
  const target = parseTarget(body.target);
  authorizeTarget(credential, target);

  const token = await createInstallationToken(env, credential.installationId);
  await deleteRunner(token, target, runnerId);
  return new Response(null, { status: 204 });
}

async function handleListRunners(request: Request, env: Env): Promise<Response> {
  const credential = await authenticate(env, request.headers.get("Authorization"));
  const body = (await request.json()) as { target: Target; prefix?: string };
  const target = parseTarget(body.target);
  authorizeTarget(credential, target);

  const token = await createInstallationToken(env, credential.installationId);
  const runners = await listRunners(token, target, body.prefix ?? "");
  return Response.json({ runners });
}

async function handleRegisterExchange(request: Request, env: Env): Promise<Response> {
  const userToken = request.headers.get("Authorization")?.replace(/^Bearer\s+/i, "");
  if (!userToken) {
    return new Response("unauthorized", { status: 401 });
  }
  const body = (await request.json()) as { target: Target };
  const target = parseTarget(body.target);

  const installationId = await verifyUserAccess(env, userToken, target);
  const credential = await issueHostJWT(env, installationId, target);
  return Response.json({ credential, target });
}

async function verifyUserAccess(
  env: Env,
  userToken: string,
  target: Target,
): Promise<number> {
  const installations = await fetch("https://api.github.com/user/installations", {
    headers: githubUserHeaders(userToken),
  });
  if (!installations.ok) {
    throw new Response("unauthorized", { status: 401 });
  }
  const body = (await installations.json()) as {
    installations: Array<{
      id: number;
      app_id: number;
      account?: { login: string };
    }>;
  };

  const match = body.installations.find(
    (item) =>
      item.app_id === Number(env.GITHUB_APP_ID) &&
      item.account?.login === target.org,
  );
  if (!match) {
    throw new Response("installation not found", { status: 404 });
  }

  const membership = await fetch(
    `https://api.github.com/user/memberships/orgs/${target.org}`,
    { headers: githubUserHeaders(userToken) },
  );
  if (!membership.ok) {
    throw new Response("forbidden", { status: 403 });
  }
  const info = (await membership.json()) as { role?: string; state?: string };
  if (info.state !== "active" || info.role !== "admin") {
    throw new Response("forbidden", { status: 403 });
  }
  return match.id;
}

function githubUserHeaders(userToken: string): HeadersInit {
  return {
    Authorization: `Bearer ${userToken}`,
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
  };
}
