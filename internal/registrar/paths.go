package registrar

import "fmt"

const (
	BrokerJITConfigPath   = "/v1/jitconfig"
	BrokerRunnersListPath = "/v1/runners/list"
)

func BrokerRunnerPath(runnerID int64) string {
	return fmt.Sprintf("/v1/runners/%d", runnerID)
}
