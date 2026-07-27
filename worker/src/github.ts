import type { Env, Target } from "./types";

const GITHUB_API = "https://api.github.com";

export async function createInstallationToken(
  env: Env,
  installationId: number,
): Promise<string> {
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
    throw new Error(`installation token failed: ${resp.status}`);
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
    throw new Error(`jit failed: ${resp.status} ${JSON.stringify(body)}`);
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
    throw new Error(`delete runner failed: ${resp.status}`);
  }
}

export async function listRunners(
  installationToken: string,
  target: Target,
  prefix: string,
): Promise<Array<{ id: number; name: string }>> {
  const runners: Array<{ id: number; name: string }> = [];
  let page = 1;
  for (;;) {
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
      throw new Error(`list runners failed: ${resp.status}`);
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
      break;
    }
    page++;
  }
  return runners;
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
  const key = await importPKCS8(env.GITHUB_APP_PRIVATE_KEY, "RS256");
  const signature = await crypto.subtle.sign(
    "RSASSA-PKCS1-v1_5",
    key,
    new TextEncoder().encode(data),
  );
  return `${data}.${base64url(signature)}`;
}

async function importPKCS8(pem: string, alg: string): Promise<CryptoKey> {
  const normalized = pem.replace(/\\n/g, "\n");
  const body = normalized
    .replace("-----BEGIN PRIVATE KEY-----", "")
    .replace("-----END PRIVATE KEY-----", "")
    .replace(/\s+/g, "");
  const raw = Uint8Array.from(atob(body), (c) => c.charCodeAt(0));
  return crypto.subtle.importKey(
    "pkcs8",
    raw,
    { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" },
    false,
    ["sign"],
  );
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
      "X-GitHub-Api-Version": "2022-11-28",
      ...(init.headers ?? {}),
    },
  });
}

export function parseTarget(raw: unknown): Target {
  const value = raw as Record<string, unknown>;
  if (value.type !== "org") {
    throw new Error("invalid target");
  }
  return {
    type: "org",
    org: String(value.org),
    runner_group_id: Number(value.runner_group_id),
  };
}

export function targetKey(target: Target): string {
  return `org:${target.org}:${target.runner_group_id}`;
}
