package settings

// How much runs at once and how hard the app tries: the numbers the dispatcher
// reads on every pass.

func sanitizeQueue(n Settings) Settings {
	if n.MaxConcurrent < 1 {
		n.MaxConcurrent = 1
	}
	if n.MaxConcurrent > 64 {
		n.MaxConcurrent = 64
	}
	if n.MaxPerHost < 1 {
		n.MaxPerHost = 1
	}
	if n.MaxPerHost > n.MaxConcurrent {
		n.MaxPerHost = n.MaxConcurrent
	}
	if n.SpeedLimit < 0 {
		n.SpeedLimit = 0
	}
	if n.MaxRetries < 0 {
		n.MaxRetries = 0
	}
	if n.MaxRetries > 20 {
		n.MaxRetries = 20
	}
	return n
}
