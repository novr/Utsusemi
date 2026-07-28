import type { Env, Target } from "./types";
import { HttpError } from "./http";
import { pemToPkcs8 } from "./pem";

const GITHUB_API = "https://api.github.com";
const USER_AGENT = "utsusemi-broker";
const MAX_INSTALLATION_PAGES = 10;
const MAX_RUNNER_PAGES = 20;

export async function createInstallationToken(
  env: Env,
  installationId: number,
): Promise<string> {
  if (!env.GITHUB_APP_PRIVATE_KEY) {
    throw new HttpError(500, "github app private key is not configured");
  }
  const appJWT = await signAppJWT(env);
  const resp = await githubFetch(
    `${GITHUB_API}/app/installations/${installationId}/access_tokens`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${appJWT}`,
        Accept: "application/vnd.github+json",
      },
    },
  );
  const body = (await resp.json()) as { token?: string };
  if (!resp.ok || !body.token) {
    console.error("installation token failed", resp.status);
    throw new HttpError(502, "github api error");
  }
  return body.token;
}

export async function createJIT(
  installationToken: string,
  target: Target,
  labels: string[],
  name: string,
): Promise<{ encoded_jit_config: string; runner: { id: number; name: string } }> {
  const resp = await githubFetch(
    `${GITHUB_API}/orgs/${target.org}/actions/runners/generate-jitconfig`,
    {
      method: "POST",
      headers: {
        Authorization: `Bearer ${installationToken}`,
        Accept: "application/vnd.github+json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        name,
        runner_group_id: target.runner_group_id,
        labels,
        ephemeral: true,
        disable_update: true,
      }),
    },
  );
  const body = await resp.json();
  if (!resp.ok) {
    console.error("jit failed", resp.status, body);
    throw new HttpError(502, "github api error");
  }
  return body as {
    encoded_jit_config: string;
    runner: { id: number; name: string };
  };
}

export async function deleteRunner(
  installationToken: string,
  target: Target,
  runnerId: number,
): Promise<void> {
  const resp = await githubFetch(
    `${GITHUB_API}/orgs/${target.org}/actions/runners/${runnerId}`,
    {
      method: "DELETE",
      headers: {
        Authorization: `Bearer ${installationToken}`,
        Accept: "application/vnd.github+json",
      },
    },
  );
  if (!resp.ok && resp.status !== 404) {
    console.error("delete runner failed", resp.status);
    throw new HttpError(502, "github api error");
  }
}

export async function listRunners(
  installationToken: string,
  target: Target,
  prefix: string,
): Promise<Array<{ id: number; name: string }>> {
  const runners: Array<{ id: number; name: string }> = [];
  for (let page = 1; page <= MAX_RUNNER_PAGES; page++) {
    const resp = await githubFetch(
      `${GITHUB_API}/orgs/${target.org}/actions/runners?per_page=100&page=${page}`,
      {
        headers: {
          Authorization: `Bearer ${installationToken}`,
          Accept: "application/vnd.github+json",
        },
      },
    );
    if (!resp.ok) {
      console.error("list runners failed", resp.status);
      throw new HttpError(502, "github api error");
    }
    const body = (await resp.json()) as {
      runners: Array<{ id: number; name: string }>;
    };
    for (const runner of body.runners) {
      if (!prefix || runner.name.startsWith(prefix)) {
        runners.push(runner);
      }
    }
    if (body.runners.length < 100) {
      return runners;
    }
  }
  console.error("list runners exceeded page limit");
  throw new HttpError(502, "github api error");
}

export async function findAppInstallation(
  env: Env,
  userToken: string,
  org: string,
): Promise<number | null> {
  const appId = Number(env.GITHUB_APP_ID);
  const wanted = org.toLowerCase();

  for (let page = 1; page <= MAX_INSTALLATION_PAGES; page++) {
    const resp = await githubFetch(
      `${GITHUB_API}/user/installations?per_page=100&page=${page}`,
      {
        headers: {
          Authorization: `Bearer ${userToken}`,
          Accept: "application/vnd.github+json",
        },
      },
    );
    if (!resp.ok) {
      throw new HttpError(401, "unauthorized");
    }
    const body = (await resp.json()) as {
      installations: Array<{
        id: number;
        app_id: number;
        account?: { login: string };
      }>;
      total_count?: number;
    };

    const match = body.installations.find(
      (item) =>
        item.app_id === appId && item.account?.login?.toLowerCase() === wanted,
    );
    if (match) {
      return match.id;
    }
    if (body.installations.length < 100) {
      return null;
    }
  }
  return null;
}

export async function signAppJWT(env: Env): Promise<string> {
  const now = Math.floor(Date.now() / 1000);
  const header = base64url(JSON.stringify({ alg: "RS256", typ: "JWT" }));
  const payload = base64url(
    JSON.stringify({
      iat: now - 60,
      exp: now + 9 * 60,
      iss: env.GITHUB_APP_ID,
    }),
  );
  const data = `${header}.${payload}`;
  const key = await importPKCS8(env.GITHUB_APP_PRIVATE_KEY);
  const signature = await crypto.subtle.sign(
    "RSASSA-PKCS1-v1_5",
    key,
    new TextEncoder().encode(data),
  );
  return `${data}.${base64url(signature)}`;
}

async function importPKCS8(pem: string): Promise<CryptoKey> {
  try {
    return await crypto.subtle.importKey(
      "pkcs8",
      pemToPkcs8(pem),
      { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
      false,
      ["sign"],
    );
  } catch (err) {
    console.error("github app private key import failed", err);
    throw new HttpError(500, "github app private key is invalid");
  }
}

function base64url(input: string | ArrayBuffer): string {
  const bytes =
    typeof input === "string"
      ? new TextEncoder().encode(input)
      : new Uint8Array(input);
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

async function githubFetch(url: string, init: RequestInit): Promise<Response> {
  return fetch(url, {
    ...init,
    headers: {
      "User-Agent": USER_AGENT,
      "X-GitHub-Api-Version": "2022-11-28",
      ...(init.headers ?? {}),
    },
  });
}

export function parseTarget(raw: unknown): Target {
  if (!raw || typeof raw !== "object") {
    throw new HttpError(400, "invalid target");
  }
  const value = raw as Record<string, unknown>;
  if (value.type !== "org") {
    throw new HttpError(400, "invalid target");
  }
  const org = String(value.org ?? "")
    .trim()
    .toLowerCase();
  const runnerGroupID = Number(value.runner_group_id);
  if (!org || !Number.isInteger(runnerGroupID) || runnerGroupID <= 0) {
    throw new HttpError(400, "invalid target");
  }
  return {
    type: "org",
    org,
    runner_group_id: runnerGroupID,
  };
}

export function targetKey(target: Target): string {
  return `org:${target.org}:${target.runner_group_id}`;
}
