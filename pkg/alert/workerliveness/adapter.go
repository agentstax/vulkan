package workerliveness

func toWorkerLivenessMetadata(cfg *WorkerLivenessConfig) *workerLivenessMetadata {
	return &workerLivenessMetadata{
		RepeatInterval: cfg.RepeatInterval,
	}
}
