export interface Env {
  GITHUB_APP_ID: string;
  GITHUB_APP_PRIVATE_KEY: string;
  BROKER_API_KEY?: string;
  ALLOWED_TARGETS?: string;
  CREDENTIAL_SIGNING_PRIVATE_KEY?: string;
  JWT_ISSUER: string;
  JWT_AUDIENCE: string;
  JWT_VERSION: string;
}

export type Target =
  | { type: "org"; org: string; runner_group_id: number }
  | { type: "repo"; owner: string; repo: string };

export interface JITRequest {
  target: Target;
  labels: string[];
  name: string;
}

export interface HostCredential {
  mode: "api_key" | "jwt";
  value: string;
  target?: Target;
}
