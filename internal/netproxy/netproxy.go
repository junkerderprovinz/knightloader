// Package netproxy runs a loopback HTTP proxy that all downloads are pointed
// at. Its only job is to be a place where the speed limit can be enforced: the
// embedded download engine offers no rate-limit hook, but it does let us set a
// proxy, so the bytes come past here on their way in.
//
// It listens on 127.0.0.1 only and is never exposed.
//
// It is the meter, and being the meter is the whole of it: this proxy chains
// into nothing. It dials the target itself, which is why a download that comes
// through here is both metered and unproxied - and why "the direct gateway" and
// "the loopback proxy" are the same path, not two.
//
// The hazard that follows from that is worth stating where somebody reading this
// package will find it. Per-download routing works by naming a proxy on the
// download request itself, and gopeed resolves the request's proxy INSTEAD of
// the global one, not in addition to it. So a routed download does not come past
// here at all and is not metered. That is a deliberate trade in this build (see
// Engine.DownloadVia); the reason it is not fixed here is that carrying an
// upstream proxy per download would need this one listener to learn which
// download each connection belongs to, and a request carries nothing that says
// so. The arrangement that answers it is a listener per task, which is what the
// bandwidth budget is being built around - not a chain bolted onto this file.
package netproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/throttle"
)

// Server is the loopback proxy.
type Server struct {
	ln  net.Listener
	lim *throttle.Limiter
	srv *http.Server

	transport *http.Transport

	mu     sync.Mutex
	closed bool
}

// Start brings the proxy up on a free loopback port.
func Start(lim *throttle.Limiter) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		ln:  ln,
		lim: lim,
		transport: &http.Transport{
			Proxy:                 nil, // we are the proxy; never chain into ourselves
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.serve)}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// Addr is the host:port to configure as the HTTP proxy.
func (s *Server) Addr() string {
	if s == nil || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.tunnel(w, r)
		return
	}
	s.forward(w, r)
}

// tunnel handles HTTPS: the client asks for a raw pipe to host:port, so there is
// nothing to inspect — the bytes coming back are simply metered on their way
// through.
func (s *Server) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := net.DialTimeout("tcp", hostPort(r.Host, "443"), 30*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: connection cannot be hijacked", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	client, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()

	// Anything the client already buffered belongs upstream before the pipe runs.
	if buf != nil && buf.Reader.Buffered() > 0 {
		if _, err := io.CopyN(upstream, buf, int64(buf.Reader.Buffered())); err != nil {
			return
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	done := make(chan struct{}, 2)
	// Upload is not metered: the limit is about download bandwidth.
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = s.lim.Copy(ctx, client, upstream); done <- struct{}{} }()
	<-done
}

// forward handles plain HTTP proxy requests.
func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		http.Error(w, "proxy: absolute URI required", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	stripHopByHop(out.Header)

	resp, err := s.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	h := w.Header()
	for k, vs := range resp.Header {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	stripHopByHop(h)
	w.WriteHeader(resp.StatusCode)
	if _, err := s.lim.Copy(r.Context(), w, resp.Body); err != nil && !errors.Is(err, context.Canceled) {
		return
	}
}

// hopByHop headers are per-connection and must not be passed along.
var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func stripHopByHop(h http.Header) {
	for _, k := range hopByHop {
		h.Del(k)
	}
}

// hostPort adds the default port when the CONNECT target omits it.
func hostPort(host, defPort string) string {
	if strings.LastIndex(host, ":") > strings.LastIndex(host, "]") {
		return host
	}
	return fmt.Sprintf("%s:%s", host, defPort)
}
