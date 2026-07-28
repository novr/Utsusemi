export const brokerJITConfigPath = "/v1/jitconfig";
export const brokerRunnersListPath = "/v1/runners/list";
export const brokerCredentialExchangePath = "/v1/credentials/exchange";

export function brokerRunnerPath(runnerID: number): string {
  return `/v1/runners/${runnerID}`;
}
