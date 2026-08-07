package api

// Link containers: the .txt/.dlc/.ccf/.rsdf files people are handed instead of
// links.
//
// A plain list is parsed here and staged like any paste. An encrypted one is
// not decrypted here and never will be: the key is issued by a service to
// registered clients, and borrowing somebody else's application key to pretend
// to be their client is not something this project does. It goes to the headless
// JDownloader backend, which has its own key and does this legitimately.
//
// Handing it over is the awkward part, and the shape of this file is entirely
// about it. JD's API takes links, not files: a filesystem path would have to
// name a file on JD's own machine, which in the normal deployment is a different
// container with a different filesystem. So the uploaded bytes are served back
// out at an address JD can fetch, once, briefly, and JD is given that address.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/container"
	"github.com/junkerderprovinz/knightloader/internal/core"
)

// relayTTL is how long a handed-over container stays fetchable. Long enough for
// a busy JD to get to it, short enough that the window in which the address
// exists at all is measured in minutes.
const relayTTL = 2 * time.Minute

// relayed is one container waiting to be collected.
type relayed struct {
	name    string
	data    []byte
	expires time.Time
}

// containerRelay hands out one-shot addresses for uploaded containers.
//
// The address is the credential, which is why it is 32 bytes from crypto/rand
// and why the route is exempt from the session check: the fetch comes from JD,
// on another host, with no cookie and no way to be given one. Three things keep
// that honest — the token is unguessable, it is spent on first use, and it
// expires either way — so the worst an attacker can do with the route is fetch
// bytes they would have to have guessed a 256-bit number to name, which the user
// themselves uploaded moments earlier.
type containerRelay struct {
	mu    sync.Mutex
	items map[string]relayed
}

func newContainerRelay() *containerRelay {
	return &containerRelay{items: map[string]relayed{}}
}

// put stores the bytes and returns the token to fetch them with. The token
// carries the original extension because that is how JD recognises the format;
// the name itself is not used as a path component anywhere.
func (cr *containerRelay) put(name string, data []byte) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	if ext := containerExt(name); ext != "" {
		token += "." + ext
	}
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.sweepLocked()
	cr.items[token] = relayed{name: name, data: data, expires: time.Now().Add(relayTTL)}
	return token, nil
}

// take returns the bytes and forgets them. One fetch is all a backend needs, and
// an address that keeps working is an address that can be replayed.
func (cr *containerRelay) take(token string) (relayed, bool) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.sweepLocked()
	it, ok := cr.items[token]
	if !ok {
		return relayed{}, false
	}
	delete(cr.items, token)
	return it, true
}

// sweepLocked drops what nobody collected. Without it a JD that is unreachable
// leaves every failed handover in memory for the life of the process, and each
// one is up to eight megabytes.
func (cr *containerRelay) sweepLocked() {
	now := time.Now()
	for k, it := range cr.items {
		if now.After(it.expires) {
			delete(cr.items, k)
		}
	}
}

// containerExt is the file's extension, lower-cased, and only when it is one of
// the container formats. Anything else is dropped rather than echoed into a URL
// this server hands out.
func containerExt(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	switch ext {
	case "dlc", "ccf", "rsdf":
		return ext
	}
	return ""
}

func registerContainers(reg *Registry, a *app.App) {
	relay := newContainerRelay()

	reg.Add(http.MethodPost, "/api/containers", "upload a link container: a text list is staged, an encrypted one goes to the JD backend",
		func(w http.ResponseWriter, r *http.Request) {
			// The cap is on the request, not on the part: without it the multipart
			// reader will happily buffer whatever is sent before the size of the file
			// inside it is known.
			r.Body = http.MaxBytesReader(w, r.Body, container.MaxBytes+1<<20)
			file, header, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "send the container as a multipart form field named \"file\"", http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, container.MaxBytes+1))
			if err != nil {
				http.Error(w, "could not read the uploaded file", http.StatusBadRequest)
				return
			}
			if len(data) > container.MaxBytes {
				http.Error(w, fmt.Sprintf("a container over %d bytes is not one", container.MaxBytes), http.StatusRequestEntityTooLarge)
				return
			}
			name := path.Base(header.Filename)
			pkg := r.FormValue("package")

			links, err := container.Links(name, data)
			switch {
			case err == nil:
				created := a.AddLinksFrom(links, pkg, app.OriginContainer)
				if created == nil {
					created = []*core.Task{} // an empty result is [] for clients, never null
				}
				writeJSON(w, map[string]any{
					"kind":    container.Detect(name, data),
					"links":   len(links),
					"created": created,
				})
			case errors.Is(err, container.ErrNeedsBackend):
				handToJD(w, r, a, relay, name, data, pkg)
			default:
				// Verbatim, because the container package's errors are written to be
				// read: "too short to be a DLC", "not a link list or a container we
				// recognise". A generic failure here is what leaves somebody
				// re-downloading the same broken file.
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		})

	// The collection address. Open, because the fetch comes from the JD backend
	// on another host, with no session and no way to be given one — the token in
	// the path is the credential, and it is unguessable, single-use and
	// short-lived. See containerRelay.
	reg.AddOpen(http.MethodGet, "/api/containers/relay/{token}",
		"hand an uploaded container to the backend that can decrypt it; the unguessable single-use token is the credential",
		func(w http.ResponseWriter, r *http.Request) {
			it, ok := relay.take(r.PathValue("token"))
			if !ok {
				http.Error(w, "this handover has already been collected or has expired", http.StatusNotFound)
				return
			}
			// A fixed type from our own side and nosniff: the bytes are whatever was
			// uploaded, and nothing about them may be allowed to decide how a client
			// treats them.
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Disposition", "attachment; filename="+quoteFilename(it.name))
			_, _ = w.Write(it.data)
		})
}

// handToJD publishes the container at a fetchable address and points JD at it.
func handToJD(w http.ResponseWriter, r *http.Request, a *app.App, relay *containerRelay, name string, data []byte, pkg string) {
	// Asked before anything is stored, so that an instance with no JD says so
	// instead of leaving a handover nobody will ever collect. The refusal names
	// the reason: an encrypted container is not a file this app failed to read.
	if !a.ContainerBackendConfigured() {
		http.Error(w, app.ErrNoContainerBackend.Error(), http.StatusServiceUnavailable)
		return
	}
	token, err := relay.put(name, data)
	if err != nil {
		http.Error(w, "could not prepare the handover", http.StatusInternalServerError)
		return
	}
	url := requestOrigin(r) + "/api/containers/relay/" + token
	if err := a.HandContainerToJD(url, name, pkg); err != nil {
		_, _ = relay.take(token) // nothing is going to collect it now
		http.Error(w, "the JDownloader backend refused the container: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]any{
		"kind":      container.Detect(name, data),
		"handedTo":  "jd",
		"expiresIn": int(relayTTL.Seconds()),
	})
}

// requestOrigin is the address this instance was reached on, which is the
// address the backend has to fetch from. It is derived from the request rather
// than configured: a NAS has several, and the one that works is the one the
// browser just used.
func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// quoteFilename puts a name in a Content-Disposition header without letting it
// end the header. Anything unusual is dropped rather than escaped: the name is
// a convenience for the backend, and no part of the app depends on it.
func quoteFilename(name string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range name {
		switch {
		case r < 32 || r == 127, r == '"', r == '\\':
		case r > 126:
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
