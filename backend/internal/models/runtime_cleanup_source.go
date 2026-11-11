package models

// RuntimeCleanupSource is retained publication history, not stop authority by
// itself. StoppedProof is set only after a trusted durable receipt is validated.
type RuntimeCleanupSource struct {
	Generation    int
	RuntimePodID  int64
	PodUID        string
	Endpoint      string
	GatewayID     string
	WorkspacePath string
	StoppedProof  bool
	Blocker       string
	// A healthy legacy recovery cleared its phase without retaining whether
	// placement advanced generation. The verifier must prove Generation stopped
	// and match this source to Generation or Generation-1 using retained receipts.
	RecoveryGenerationUncertain bool
}
