package settings

// AfterDownload names one job on one ArrowLoop instance of the fortress.
type AfterDownload struct {
	// Member is the instance's member id in the fortress.
	Member string `json:"member"`
	// Device is the instance's device name when it was picked, to name it
	// while it is away.
	Device string `json:"device,omitempty"`
	Job    string `json:"job"`
}

// maxAfterDownloadName holds the names to what a picker row can show.
const maxAfterDownloadName = 128

// sanitized is a fresh copy with its names trimmed, or nil when it names no
// instance or no job.
func (a *AfterDownload) sanitized() *AfterDownload {
	if a == nil {
		return nil
	}
	out := AfterDownload{
		Member: trimTo(a.Member, maxAfterDownloadName),
		Device: trimTo(a.Device, maxAfterDownloadName),
		Job:    trimTo(a.Job, maxAfterDownloadName),
	}
	if out.Member == "" || out.Job == "" {
		return nil
	}
	return &out
}
