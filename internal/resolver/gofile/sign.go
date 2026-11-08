package gofile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dop251/goja"
)

// scriptMaxAge is how long a copy of the website's signing script is used.
// Gofile rotates the secret in it and stops taking a retired one six hours
// after a rotation, banning the address that sends it, so a copy is fetched
// again well before that.
var scriptMaxAge = time.Hour

// scriptTimeout bounds one run of the script, which comes from the network
// and so could loop.
const scriptTimeout = 5 * time.Second

// maxScript caps how much of the script is read. Gofile's is about 40 KB.
const maxScript = 1 << 20

// language is what the website sends as the browser's language, in the
// signature and beside it.
const language = "en-US"

// signer runs Gofile's own signing script, /js/wt.obf.js, the way the website
// does. Its secret changes, so it is not copied here.
type signer struct {
	vm      *goja.Runtime
	sign    goja.Callable
	fetched time.Time
}

// websiteToken is the X-Website-Token the website sends with a request made
// with token.
func (c *Client) websiteToken(ctx context.Context, token string) (string, error) {
	c.signMu.Lock()
	defer c.signMu.Unlock()
	if c.signer == nil || c.now().Sub(c.signer.fetched) > scriptMaxAge {
		s, err := c.loadSigner(ctx)
		if err != nil {
			return "", err
		}
		c.signer = s
	}
	vm := c.signer.vm
	timer := time.AfterFunc(scriptTimeout, func() { vm.Interrupt("gofile: the signing script ran too long") })
	v, err := c.signer.sign(goja.Undefined(), vm.ToValue(token))
	timer.Stop()
	vm.ClearInterrupt()
	if err != nil {
		c.signer = nil
		return "", fmt.Errorf("gofile: the website's signing script failed: %w", err)
	}
	return v.String(), nil
}

func (c *Client) loadSigner(ctx context.Context) (*signer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site+"/js/wt.obf.js", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gofile: the website's signing script answered %d", resp.StatusCode)
	}
	src, err := io.ReadAll(io.LimitReader(resp.Body, maxScript))
	if err != nil {
		return nil, err
	}

	vm := goja.New()
	nav := vm.NewObject()
	nav.Set("userAgent", c.ua)
	nav.Set("language", language)
	vm.Set("navigator", nav)
	vm.Set("window", vm.GlobalObject())
	timer := time.AfterFunc(scriptTimeout, func() { vm.Interrupt("gofile: the signing script ran too long") })
	_, err = vm.RunString(string(src))
	timer.Stop()
	vm.ClearInterrupt()
	if err != nil {
		return nil, fmt.Errorf("gofile: the website's signing script does not run: %w", err)
	}
	sign, ok := goja.AssertFunction(vm.Get("generateWT"))
	if !ok {
		return nil, errors.New("gofile: the website's signing script has no generateWT")
	}
	return &signer{vm: vm, sign: sign, fetched: c.now()}, nil
}
