package spawn

type Result struct {
	JobMs       int64
	ColdStartMs int64
	TotalMs     int64
}

func resultFromMetrics(m LastSpawn) Result {
	coldStart := m.coldStartMs()
	return Result{
		JobMs:       m.JobMs,
		ColdStartMs: coldStart,
		TotalMs:     coldStart + m.JobMs,
	}
}
