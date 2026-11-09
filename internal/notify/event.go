package notify

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	kit "github.com/junkerderprovinz/parleyport/kit/notify"

	"github.com/junkerderprovinz/knightloader/internal/script"
)

// Event is what a channel is told about one firing. The texts are English:
// the server does not know the reader's language. Fields carries every
// placeholder this build fills in, so a webhook template keeps its names.
func Event(f script.Firing, instanceName string) kit.Event {
	e := kit.Event{
		Kind:     string(f.Trigger),
		Severity: severity(f),
		At:       f.At,
		Fields:   Fields(f, instanceName),
	}
	if instanceName != "" {
		e.Facts = append(e.Facts, kit.Fact{Name: "Instance", Value: instanceName})
	}
	fact := func(name, value string) {
		if value != "" {
			e.Facts = append(e.Facts, kit.Fact{Name: name, Value: value})
		}
	}
	switch {
	case f.Task != nil && (f.Trigger == script.TriggerTaskDone || f.Trigger == script.TriggerTaskFailed ||
		f.Trigger == script.TriggerLinkAdded || f.Trigger == script.TriggerChecksumFailed):
		e.Title = map[script.Trigger]string{
			script.TriggerTaskDone:       "Download finished",
			script.TriggerTaskFailed:     "Download failed",
			script.TriggerLinkAdded:      "Link added",
			script.TriggerChecksumFailed: "Checksum mismatch",
		}[f.Trigger]
		v := f.Task
		e.Body = firstOf(v.Name, v.URL)
		fact("Package", v.Package)
		fact("Size", sizeText(v.Size))
		fact("Category", v.Category)
		fact("Host", v.Host)
		if f.Trigger != script.TriggerTaskDone {
			fact("Error", firstOf(v.Error, v.Reason))
		}
	case f.Package != nil:
		v := f.Package
		e.Title, e.Body = "Package finished", v.Name
		if v.Failed > 0 {
			e.Title = "Package finished with failed files"
			fact("Failed", strconv.Itoa(v.Failed))
		}
		fact("Files", fmt.Sprintf("%d of %d done", v.Done, v.Files))
		fact("Size", sizeText(v.Bytes))
	case f.Extract != nil:
		v := f.Extract
		e.Title, e.Body = "Unpacked", firstOf(v.Package, v.Name)
		if !v.OK {
			e.Title = "Unpacking failed"
			fact("Error", v.Error)
		}
		fact("Folder", v.Dir)
	case f.Reconnect != nil:
		v := f.Reconnect
		switch {
		case !v.OK:
			e.Title = "Reconnect failed"
			fact("Error", v.Error)
		case v.Changed:
			e.Title = "New IP address"
		default:
			e.Title = "Reconnect kept the same IP address"
		}
		fact("From", v.From)
		fact("To", v.To)
	case f.Account != nil:
		v := f.Account
		e.Title, e.Body = "Account expired", firstOf(v.Label, v.Service)
		fact("Service", v.Service)
		fact("Expired", v.Expiry)
	case f.Captcha != nil:
		v := f.Captcha
		e.Title, e.Body = "Captcha waiting", v.Host
		fact("Prompt", v.Prompt)
	case f.Trigger == script.TriggerQueueIdle:
		e.Title = "Queue is idle"
	default:
		e.Title = string(f.Trigger)
	}
	return e
}

// severity is how good or bad a firing is, which picks the channels set to
// failures only and the colour or priority a service shows.
func severity(f script.Firing) kit.Severity {
	switch f.Trigger {
	case script.TriggerTaskDone:
		return kit.SeveritySuccess
	case script.TriggerTaskFailed, script.TriggerChecksumFailed:
		return kit.SeverityFailure
	case script.TriggerPackageDone:
		if f.Package != nil && f.Package.Failed > 0 {
			return kit.SeverityFailure
		}
		return kit.SeveritySuccess
	case script.TriggerExtractDone:
		if f.Extract != nil && !f.Extract.OK {
			return kit.SeverityFailure
		}
		return kit.SeveritySuccess
	case script.TriggerReconnectDone:
		if f.Reconnect == nil || !f.Reconnect.OK {
			return kit.SeverityFailure
		}
		return kit.SeveritySuccess
	case script.TriggerAccountExpired, script.TriggerCaptchaPending:
		return kit.SeverityWarning
	}
	return kit.SeverityInfo
}

// Test sends the made-up event to one channel right now, whether it is
// switched on or not, so a draft can be tried before it is saved.
func Test(ctx context.Context, ch kit.Channel, instanceName string, client *http.Client) kit.Result {
	d, err := kit.NewDispatcher(nil, kit.Options{Source: "KnightLoader", HTTP: client})
	if err != nil {
		return kit.Result{Channel: ch.ID, Kind: ch.Kind, Code: kit.CodeInvalid, Err: err}
	}
	return d.Test(ctx, ch, Event(SampleFiring(time.Now()), instanceName))
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// sizeText is a byte count for a person, empty when it is unknown.
func sizeText(n int64) string {
	if n <= 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
