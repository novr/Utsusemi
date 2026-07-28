import type { Env, HostCredential, Target } from "./types";
import { parseTarget, targetKey } from "./github";
import { HttpError } from "./http";
import { pemToPkcs8 } from "./pem";

interface JWTClaims {
  iss: string;
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
    throw new HttpError(401, "unauthorized");
  }
  const token = authorization.slice("Bearer ".length).trim();
  if (!token) {
    throw new HttpError(401, "unauthorized");
  }

  const claims = await verifyHostJWT(env, token);
  if (!Number.isInteger(claims.installation_id) || claims.installation_id <= 0) {
    throw new HttpError(401, "unauthorized");
  }
  return {
    target: claims.target,
    installationId: claims.installation_id,
  };
}

export function authorizeTarget(
  credential: HostCredential,
  requested: Target,
): void {
  if (targetKey(credential.target) !== targetKey(requested)) {
    throw new HttpError(403, "forbidden");
  }
}

export async function issueHostJWT(
  env: Env,
  installationId: number,
  target: Target,
): Promise<string> {
  const now = Math.floor(Date.now() / 1000);
  const claims: JWTClaims = {
    iss: env.JWT_ISSUER,
    sub: crypto.randomUUID(),
    installation_id: installationId,
    target,
    iat: now,
    nbf: now,
    exp: now + 30 * 24 * 60 * 60,
    jti: crypto.randomUUID(),
    ver: env.JWT_VERSION,
  };
  return signEd25519JWT(env.CREDENTIAL_SIGNING_PRIVATE_KEY, claims);
}

async function verifyHostJWT(env: Env, token: string): Promise<JWTClaims> {
  try {
    const [headerB64, payloadB64, signatureB64] = token.split(".");
    if (!headerB64 || !payloadB64 || !signatureB64) {
      throw new HttpError(401, "unauthorized");
    }
    const header = JSON.parse(atobUrl(headerB64)) as { alg?: string };
    if (header.alg !== "EdDSA") {
      throw new HttpError(401, "unauthorized");
    }

    const publicKey = await importEd25519PublicKey(env.CREDENTIAL_SIGNING_PRIVATE_KEY);
    const valid = await crypto.subtle.verify(
      "Ed25519",
      publicKey,
      decodeBase64Url(signatureB64),
      new TextEncoder().encode(`${headerB64}.${payloadB64}`),
    );
    if (!valid) {
      throw new HttpError(401, "unauthorized");
    }

    const claims = JSON.parse(atobUrl(payloadB64)) as JWTClaims;
    if (
      claims.iss !== env.JWT_ISSUER ||
      claims.ver !== env.JWT_VERSION
    ) {
      throw new HttpError(401, "unauthorized");
    }
    const now = Math.floor(Date.now() / 1000);
    if (claims.nbf > now || claims.exp <= now) {
      throw new HttpError(401, "unauthorized");
    }
    claims.target = parseTarget(claims.target);
    return claims;
  } catch (err) {
    if (err instanceof HttpError) {
      throw err;
    }
    throw new HttpError(401, "unauthorized");
  }
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
  if (!pem) {
    throw new HttpError(500, "credential signing key is not configured");
  }
  try {
    return await crypto.subtle.importKey(
      "pkcs8",
      pemToPkcs8(pem),
      { name: "Ed25519" },
      true,
      ["sign"],
    );
  } catch (err) {
    console.error("credential signing key import failed", err);
    throw new HttpError(500, "credential signing key is invalid");
  }
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
  return atob(padBase64Url(value));
}

function decodeBase64Url(value: string): Uint8Array {
  const binary = atob(padBase64Url(value));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function padBase64Url(value: string): string {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  return normalized + "=".repeat((4 - (normalized.length % 4)) % 4);
}
