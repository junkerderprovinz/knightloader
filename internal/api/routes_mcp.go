package api

// The Model Context Protocol endpoint, for assistants such as Claude Code or
// Cursor, and the routes the settings card sets clients up with. Each client
// gets an API token of its own, and /mcp lets in only tokens a client holds,
// so the key typed into Sonarr cannot drive an assistant's tools. With no
// client set up the endpoint answers 404, the same as one that does not exist.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
	"github.com/junkerderprovinz/knightloader/internal/auth"
	"github.com/junkerderprovinz/knightloader/internal/mcpclient"
)

const (
	mcpPath    = "/mcp"
	mcpMaxBody = 1 << 20
	// The JSON-RPC 2.0 code for a request the server refuses to look at.
	mcpInvalidRequest = -32600
)

// mcpCaller is the client a request came in as and the token it used, which
// is how a tool learns what it may do.
type mcpCaller struct {
	token  apitoken.Token
	client mcpclient.Client
}

type mcpCallerKey struct{}

func withMCPCaller(ctx context.Context, c mcpCaller) context.Context {
	return context.WithValue(ctx, mcpCallerKey{}, c)
}

func mcpCallerFrom(ctx context.Context) (mcpCaller, bool) {
	c, ok := ctx.Value(mcpCallerKey{}).(mcpCaller)
	return c, ok
}

func registerMCP(reg *Registry, a *app.App) {
	srv := newMCPServer(a)
	transport := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		// Only a request that skipped the gate arrives without a caller, and
		// the SDK answers it 400.
		if _, ok := mcpCallerFrom(r.Context()); !ok {
			return nil
		}
		return srv
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// The SDK's rule refuses a loopback address reached under another
		// name, which is what a reverse proxy on the same host looks like.
		// knownHost and the token already stand where that rule would.
		DisableLocalhostProtection:   true,
		MaxRequestBodyBytes:          mcpMaxBody,
		PropagateRequestCancellation: true,
	})

	reg.AddOpen(AnyMethod, mcpPath,
		"the Model Context Protocol endpoint (Streamable HTTP) for AI assistants; answers 404 until a client "+
			"is set up under Settings > Security > MCP server, and takes only the API token made for that client",
		func(w http.ResponseWriter, r *http.Request) { serveMCP(a, transport, w, r) })

	reg.Add(http.MethodGet, "/api/mcp",
		"the MCP endpoint's path, the AI clients set up for it with their token's name, rights and last call, and the tools it offers",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, mcpOverview(a))
		})

	reg.Add(http.MethodPost, "/api/mcp/clients",
		"set up an AI client: issues a token named name with the rights in scopes and ties it to the client kind; "+
			"the secret is in this one response and nowhere else",
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Kind   string           `json:"kind"`
				Name   string           `json:"name"`
				Scopes []apitoken.Scope `json:"scopes"`
			}
			if !decodeJSON(w, r, &body) {
				return
			}
			scopes, err := apitoken.Normalize(body.Scopes)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if own, ok := tokenOf(r); ok {
				for _, s := range scopes {
					if !own.Has(s) {
						refuseScope(w, s)
						return
					}
				}
			}
			var client mcpclient.Client
			var tok apitoken.Token
			var secret string
			err = a.Auth.Mint(stillAdmitted(a, r), func() error {
				var err error
				if tok, secret, err = a.APITokens.CreateScoped(body.Name, scopes); err != nil {
					return err
				}
				if client, err = a.MCPClients.Add(body.Kind, tok.ID); err != nil {
					_ = a.APITokens.Revoke(tok.ID)
				}
				return err
			})
			switch {
			case errors.Is(err, auth.ErrStale):
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			case errors.Is(err, apitoken.ErrEmptyName), errors.Is(err, apitoken.ErrTooMany),
				errors.Is(err, mcpclient.ErrNoKind), errors.Is(err, mcpclient.ErrTooMany):
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			case err != nil:
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSONStatus(w, http.StatusCreated, map[string]any{
				"client": mcpClientViewOf(client, &tok),
				"secret": secret,
			})
		})

	reg.Add(http.MethodDelete, "/api/mcp/clients/{id}",
		"remove an AI client and revoke the token made for it; the endpoint closes with the last one",
		func(w http.ResponseWriter, r *http.Request) {
			c, err := a.MCPClients.Remove(r.PathValue("id"))
			switch {
			case errors.Is(err, mcpclient.ErrNotFound):
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			case err != nil:
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := a.APITokens.Revoke(c.TokenID); err != nil && !errors.Is(err, apitoken.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
}

// mcpClientView is one client as the settings card shows it.
type mcpClientView struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	TokenID   string           `json:"tokenId"`
	TokenName string           `json:"tokenName"`
	Scopes    []apitoken.Scope `json:"scopes"`
	AddedAt   time.Time        `json:"addedAt"`
	LastCall  *time.Time       `json:"lastCall,omitempty"`
	// TokenGone is set once the token was revoked elsewhere, by hand or by a
	// password change; the client is then locked out until it is set up again.
	TokenGone bool `json:"tokenGone,omitempty"`
}

func mcpClientViewOf(c mcpclient.Client, tok *apitoken.Token) mcpClientView {
	v := mcpClientView{ID: c.ID, Kind: c.Kind, TokenID: c.TokenID, AddedAt: c.AddedAt, LastCall: c.LastCall}
	if tok == nil {
		v.TokenGone = true
		v.Scopes = []apitoken.Scope{}
		return v
	}
	v.TokenName, v.Scopes = tok.Name, tok.Scopes
	return v
}

type mcpToolView struct {
	Name  string         `json:"name"`
	Title string         `json:"title"`
	Scope apitoken.Scope `json:"scope"`
}

type mcpOverviewView struct {
	Path    string          `json:"path"`
	Clients []mcpClientView `json:"clients"`
	Tools   []mcpToolView   `json:"tools"`
}

func mcpOverview(a *app.App) mcpOverviewView {
	tokens := map[string]apitoken.Token{}
	for _, t := range a.APITokens.List() {
		tokens[t.ID] = t
	}
	out := mcpOverviewView{Path: mcpPath, Clients: []mcpClientView{}}
	for _, c := range a.MCPClients.List() {
		var tok *apitoken.Token
		if t, ok := tokens[c.TokenID]; ok {
			tok = &t
		}
		out.Clients = append(out.Clients, mcpClientViewOf(c, tok))
	}
	for _, d := range mcpToolDefs(a) {
		out.Tools = append(out.Tools, mcpToolView{Name: d.tool.Name, Title: d.tool.Title, Scope: d.scope})
	}
	return out
}

// serveMCP is the gate in front of the transport: a client set up at all, a
// token, a token that belongs to a client, and one JSON-RPC message per
// request, in that order.
func serveMCP(a *app.App, transport http.Handler, w http.ResponseWriter, r *http.Request) {
	// The wording matches the /api/ catch-all, so a closed endpoint does not
	// say whether a token would have worked.
	if a.MCPClients.Empty() {
		http.Error(w, "no such endpoint: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	secret, conflict := mcpSecret(r)
	var caller mcpCaller
	ok := false
	if secret != "" && !conflict {
		if caller.token, ok = a.APITokens.Check(secret); ok {
			caller.client, ok = a.MCPClients.ByToken(caller.token.ID)
		}
	}
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="KnightLoader MCP"`)
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{
			"error": "this needs the API token made for an MCP client under Settings > Security > MCP server",
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mcpMaxBody)
	// A body that declares more than the limit is refused for its size by the
	// transport, whatever its first byte says.
	if r.ContentLength <= mcpMaxBody && isBatchBody(r) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"jsonrpc": "2.0",
			"id":      nil,
			"error":   map[string]any{"code": mcpInvalidRequest, "message": "batch requests are not accepted"},
		})
		return
	}
	a.MCPClients.Touch(caller.client.ID, time.Now())
	transport.ServeHTTP(w, r.WithContext(withMCPCaller(r.Context(), caller)))
}

// mcpSecret reads the token from Authorization: Bearer or from X-API-Key,
// which mcp-remote's header file is written with. conflict means both came
// and they differ.
func mcpSecret(r *http.Request) (secret string, conflict bool) {
	bearer := bearerToken(r)
	key := strings.TrimSpace(r.Header.Get("X-API-Key"))
	switch {
	case bearer != "" && key != "":
		return bearer, bearer != key
	case bearer != "":
		return bearer, false
	}
	return key, false
}

// rewoundBody hands what isBatchBody read back to the transport ahead of the
// rest of the body.
type rewoundBody struct {
	io.Reader
	io.Closer
}

// isBatchBody reports whether the body's first non-whitespace byte is '[', the
// shape of a JSON-RPC batch. What it read is put back in front of the rest, and
// a read error travels on to the transport, which answers an oversized body
// with 413.
func isBatchBody(r *http.Request) bool {
	var peeked []byte
	rest := r.Body
	defer func() {
		r.Body = rewoundBody{Reader: io.MultiReader(bytes.NewReader(peeked), rest), Closer: rest}
	}()
	buf := make([]byte, 512)
	for {
		n, err := rest.Read(buf)
		peeked = append(peeked, buf[:n]...)
		if trimmed := bytes.TrimLeft(peeked, " \t\r\n"); len(trimmed) > 0 {
			return trimmed[0] == '['
		}
		if err != nil {
			return false
		}
	}
}

// decodeMCPArgs reads a tool's arguments. An unknown field is refused rather
// than ignored, so an assistant learns that KnightLoader never saw what it
// meant to pass.
func decodeMCPArgs(raw json.RawMessage, v any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New(strings.TrimPrefix(err.Error(), "json: "))
	}
	return nil
}
