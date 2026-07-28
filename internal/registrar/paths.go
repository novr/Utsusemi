package registrar

import "fmt"

const (
	brokerJITConfigPath    = "/v1/jitconfig"
	brokerRunnersListPath  = "/v1/runners/list"
)

func brokerRunnerPath(runnerID int64) string {
	return fmt.Sprintf("/v1/runners/%d", runnerID)
}
