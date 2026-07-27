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
  listInstallations,
  listInstallationRepos,
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
  authorizeTarget(env, credential, target);

  const installationId = await resolveInstallationId(env, credential, target);
  if (!installationId) {
    return new Response("installation not found", { status: 404 });
  }
  const token = await createInstallationToken(env, installationId);
  const jit = await createJIT(env, token, target, body.labels, body.name);
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
  authorizeTarget(env, credential, target);

  const installationId = await resolveInstallationId(env, credential, target);
  if (!installationId) {
    return new Response("installation not found", { status: 404 });
  }
  const token = await createInstallationToken(env, installationId);
  await deleteRunner(env, token, target, runnerId);
  return new Response(null, { status: 204 });
}

async function handleListRunners(request: Request, env: Env): Promise<Response> {
  const credential = await authenticate(env, request.headers.get("Authorization"));
  const body = (await request.json()) as { target: Target; prefix?: string };
  const target = parseTarget(body.target);
  authorizeTarget(env, credential, target);

  const installationId = await resolveInstallationId(env, credential, target);
  if (!installationId) {
    return new Response("installation not found", { status: 404 });
  }
  const token = await createInstallationToken(env, installationId);
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

async function resolveInstallationId(
  env: Env,
  credential: Awaited<ReturnType<typeof authenticate>>,
  target: Target,
): Promise<number> {
  if (credential.mode === "jwt") {
    const payload = credential.value.split(".")[1];
    const claims = JSON.parse(atobUrl(payload)) as { installation_id?: number };
    if (claims.installation_id) {
      return claims.installation_id;
    }
  }
  return lookupInstallationForTarget(env, target);
}

async function lookupInstallationForTarget(env: Env, target: Target): Promise<number> {
  const installations = await listInstallations(env);
  for (const installation of installations) {
    if (installation.app_id !== Number(env.GITHUB_APP_ID)) continue;
    if (target.type === "org" && installation.account?.login === target.org) {
      return installation.id;
    }
    if (target.type === "repo" && installation.account?.login === target.owner) {
      const repos = await listInstallationRepos(env, installation.id);
      if (repos.some((repo) => repo.full_name === `${target.owner}/${target.repo}`)) {
        return installation.id;
      }
    }
  }
  return 0;
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

  const appInstallations = body.installations.filter(
    (item) => item.app_id === Number(env.GITHUB_APP_ID),
  );
  if (appInstallations.length === 0) {
    throw new Response("installation not found", { status: 404 });
  }

  if (target.type === "org") {
    const match = appInstallations.find((item) => item.account?.login === target.org);
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
    const role = (await membership.json()) as { role?: string };
    if (role.role !== "admin") {
      throw new Response("forbidden", { status: 403 });
    }
    return match.id;
  }

  for (const installation of appInstallations) {
    if (installation.account?.login !== target.owner) {
      continue;
    }
    const repos = await fetch(
      `https://api.github.com/user/installations/${installation.id}/repositories`,
      { headers: githubUserHeaders(userToken) },
    );
    if (!repos.ok) {
      continue;
    }
    const repoBody = (await repos.json()) as {
      repositories: Array<{ full_name: string; permissions?: { admin?: boolean } }>;
    };
    const repo = repoBody.repositories.find(
      (item) => item.full_name === `${target.owner}/${target.repo}`,
    );
    if (repo?.permissions?.admin) {
      return installation.id;
    }
  }
  throw new Response("forbidden", { status: 403 });
}

function githubUserHeaders(userToken: string): HeadersInit {
  return {
    Authorization: `Bearer ${userToken}`,
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
  };
}

function atobUrl(value: string): string {
  const padded = value + "=".repeat((4 - (value.length % 4)) % 4);
  return atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
}
