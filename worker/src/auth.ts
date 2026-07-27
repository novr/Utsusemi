import type { Env, HostCredential, Target } from "./types";
import {
  constantTimeEqual,
  parseAllowedTargets,
  parseTarget,
  targetKey,
} from "./github";

interface JWTClaims {
  iss: string;
  aud: string;
  sub: string;
  installation_id: number;
  target: Target;
  iat: number;
  nbf: number;
  exp: number;
  jti: string;
  ver: string;
}

export async function authenticate(
  env: Env,
  authorization: string | null,
): Promise<HostCredential> {
  if (!authorization?.startsWith("Bearer ")) {
    throw new Response("unauthorized", { status: 401 });
  }
  const token = authorization.slice("Bearer ".length).trim();
  if (!token) {
    throw new Response("unauthorized", { status: 401 });
  }

  if (env.BROKER_API_KEY) {
    const ok = await constantTimeEqual(token, env.BROKER_API_KEY);
    if (ok) {
      return { mode: "api_key", value: token };
    }
  }

  if (env.CREDENTIAL_SIGNING_PRIVATE_KEY) {
    const claims = await verifyHostJWT(env, token);
    return { mode: "jwt", value: token, target: claims.target };
  }

  throw new Response("unauthorized", { status: 401 });
}

export function authorizeTarget(
  env: Env,
  credential: HostCredential,
  requested: Target,
): void {
  if (credential.mode === "jwt") {
    if (!credential.target || targetKey(credential.target) !== targetKey(requested)) {
      throw new Response("forbidden", { status: 403 });
    }
    return;
  }

  const allowed = parseAllowedTargets(env.ALLOWED_TARGETS);
  if (!allowed.has(targetKey(requested))) {
    throw new Response("forbidden", { status: 403 });
  }
}

export async function issueHostJWT(
  env: Env,
  installationId: number,
  target: Target,
): Promise<string> {
  const privateKey = env.CREDENTIAL_SIGNING_PRIVATE_KEY;
  if (!privateKey) {
    throw new Error("CREDENTIAL_SIGNING_PRIVATE_KEY is not configured");
  }
  const now = Math.floor(Date.now() / 1000);
  const claims: JWTClaims = {
    iss: env.JWT_ISSUER,
    aud: env.JWT_AUDIENCE,
    sub: crypto.randomUUID(),
    installation_id: installationId,
    target,
    iat: now,
    nbf: now,
    exp: now + 30 * 24 * 60 * 60,
    jti: crypto.randomUUID(),
    ver: env.JWT_VERSION,
  };
  return signEd25519JWT(privateKey, claims);
}

async function verifyHostJWT(env: Env, token: string): Promise<JWTClaims> {
  const [headerB64, payloadB64, signatureB64] = token.split(".");
  if (!headerB64 || !payloadB64 || !signatureB64) {
    throw new Response("unauthorized", { status: 401 });
  }
  const header = JSON.parse(atobUrl(headerB64)) as { alg?: string };
  if (header.alg !== "EdDSA") {
    throw new Response("unauthorized", { status: 401 });
  }
  const claims = JSON.parse(atobUrl(payloadB64)) as JWTClaims;
  if (
    claims.iss !== env.JWT_ISSUER ||
    claims.aud !== env.JWT_AUDIENCE ||
    claims.ver !== env.JWT_VERSION
  ) {
    throw new Response("unauthorized", { status: 401 });
  }
  const now = Math.floor(Date.now() / 1000);
  if (claims.nbf > now || claims.exp <= now) {
    throw new Response("unauthorized", { status: 401 });
  }

  const publicKey = await importEd25519PublicKey(env.CREDENTIAL_SIGNING_PRIVATE_KEY!);
  const valid = await crypto.subtle.verify(
    "Ed25519",
    publicKey,
    Uint8Array.from(atobUrlBytes(signatureB64), (c) => c.charCodeAt(0)),
    new TextEncoder().encode(`${headerB64}.${payloadB64}`),
  );
  if (!valid) {
    throw new Response("unauthorized", { status: 401 });
  }
  claims.target = parseTarget(claims.target);
  return claims;
}

async function signEd25519JWT(privateKeyPem: string, claims: JWTClaims): Promise<string> {
  const header = base64url(JSON.stringify({ alg: "EdDSA", typ: "JWT" }));
  const payload = base64url(JSON.stringify(claims));
  const data = `${header}.${payload}`;
  const key = await importEd25519PrivateKey(privateKeyPem);
  const signature = await crypto.subtle.sign(
    "Ed25519",
    key,
    new TextEncoder().encode(data),
  );
  return `${data}.${base64url(signature)}`;
}

async function importEd25519PrivateKey(pem: string): Promise<CryptoKey> {
  const normalized = pem.replace(/\\n/g, "\n");
  const body = normalized
    .replace("-----BEGIN PRIVATE KEY-----", "")
    .replace("-----END PRIVATE KEY-----", "")
    .replace(/\s+/g, "");
  const raw = Uint8Array.from(atob(body), (c) => c.charCodeAt(0));
  return crypto.subtle.importKey("pkcs8", raw, { name: "Ed25519" }, false, ["sign"]);
}

async function importEd25519PublicKey(privateKeyPem: string): Promise<CryptoKey> {
  const privateKey = await importEd25519PrivateKey(privateKeyPem);
  const jwk = (await crypto.subtle.exportKey("jwk", privateKey)) as JsonWebKey;
  delete jwk.d;
  jwk.key_ops = ["verify"];
  return crypto.subtle.importKey("jwk", jwk, { name: "Ed25519" }, false, ["verify"]);
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

function atobUrl(value: string): string {
  const padded = value + "=".repeat((4 - (value.length % 4)) % 4);
  return atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
}

function atobUrlBytes(value: string): string {
  return atobUrl(value);
}
