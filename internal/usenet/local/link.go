package local

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/junkerderprovinz/knightloader/internal/resolver"
)

const scheme = "nntp"

// FileLink is the link file index of job is staged under. It ends in the file
// name, so the list shows what the row is.
func FileLink(job string, index int, name string) string {
	u := url.URL{Scheme: scheme, Host: job, Path: "/" + strconv.Itoa(index) + "/" + name}
	return u.String()
}

type fileRef struct {
	job   string
	index int
	name  string
}

func parseLink(raw string) (fileRef, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != scheme || u.Host == "" {
		return fileRef{}, fmt.Errorf("%q is not the link of a file from your Usenet servers", raw)
	}
	idx, name, ok := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	n, err := strconv.Atoi(idx)
	if !ok || err != nil || n < 0 || name == "" {
		return fileRef{}, fmt.Errorf("%q names no file of an .nzb", raw)
	}
	return fileRef{job: u.Host, index: n, name: name}, nil
}

// Resolver claims the file links and nothing else. It ranks just above
// TorBox, so the priority card offers the own servers first.
type Resolver struct{}

func (Resolver) Info() resolver.Info { return resolver.Info{ID: ResolverID, Prio: 51} }

func (Resolver) Match(raw string) bool {
	_, err := parseLink(raw)
	return err == nil
}

func (Resolver) Resolve(_ context.Context, req resolver.Request) (resolver.Result, error) {
	ref, err := parseLink(req.URL)
	if err != nil {
		return resolver.Result{}, err
	}
	return resolver.Result{Name: ref.name, DirectURL: req.URL}, nil
}
