export interface Env {
  GITHUB_APP_ID: string;
  GITHUB_APP_PRIVATE_KEY: string;
  CREDENTIAL_SIGNING_PRIVATE_KEY: string;
  JWT_ISSUER: string;
  JWT_VERSION: string;
}

export type Target = {
  type: "org";
  org: string;
  runner_group_id: number;
};

export interface JITRequest {
  target: Target;
  labels: string[];
  name: string;
}

export interface HostCredential {
  target: Target;
  installationId: number;
}
