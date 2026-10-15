package spawn

type Result struct {
	JobMs        int64
	ColdStartMs  int64
	TotalMs      int64
	ReachedReady bool // true after waitUntilReady succeeds (even if JIT/job later fails)
	ClaimedJob   bool // true when runner output showed a job assignment/completion
}

func resultFromMetrics(m LastSpawn, claimed bool) Result {
	coldStart := m.coldStartMs()
	return Result{
		JobMs:        m.JobMs,
		ColdStartMs:  coldStart,
		TotalMs:      coldStart + m.JobMs,
		ReachedReady: true,
		ClaimedJob:   claimed,
	}
}
