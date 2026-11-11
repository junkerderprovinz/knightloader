package api

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/junkerderprovinz/parleyport/kit/fortress"
	"github.com/junkerderprovinz/parleyport/kit/fortressmcp"

	"github.com/junkerderprovinz/knightloader/internal/apitoken"
	"github.com/junkerderprovinz/knightloader/internal/app"
)

// fortressTools are the tools the other apps of the fortress may call here:
// adding links and steering the queue, nothing that removes or stops.
var fortressTools = []string{"add_links", "start_collector", "pause_downloads", "resume_downloads", "set_queue"}

// fortressToolsAction lets the other apps of the fortress call fortressTools,
// as far as the permission for them is on, with the rights a client set up
// for exactly those tools would have.
func fortressToolsAction(a *app.App) fortress.Action {
	return fortressmcp.Offer(newOwnMCPServer(a), fortressTools, func(ctx context.Context, from fortress.Caller) (context.Context, error) {
		return withMCPCaller(ctx, mcpCaller{token: apitoken.Token{
			Name:   from.App + " on " + from.Device,
			Scopes: []apitoken.Scope{apitoken.ScopeAdd, apitoken.ScopeControl},
		}}), nil
	})
}

// fortressMCP brings the fortress into the MCP server: the members, their
// status and log for every client, and the tools of other apps for a client
// that may control downloads here.
func fortressMCP(a *app.App) fortressmcp.Config {
	return fortressmcp.Config{
		Fortress: func() fortressmcp.Fortress {
			f, _ := a.Fortress().(fortressmcp.Fortress)
			return f
		},
		MayAct: func(ctx context.Context) bool {
			caller, _ := mcpCallerFrom(ctx)
			return caller.token.Has(apitoken.ScopeControl)
		},
	}
}

// newMCPServer builds the one server every client talks to: KnightLoader's
// own tools and the fortress it is in.
func newMCPServer(a *app.App) *mcp.Server {
	srv := newOwnMCPServer(a)
	fortressmcp.Add(srv, fortressMCP(a))
	return srv
}
