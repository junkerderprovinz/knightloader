# MCP server

KnightLoader has a built-in server for the Model Context Protocol (MCP), the
protocol AI assistants such as Claude Code, Claude Desktop, Cursor and VS Code
use to reach outside tools. Through it an assistant can read your downloads and,
with the rights you give it, add links and control the queue. BombVault's MCP
server works the same way, so a client set up for one reads the same for the
other.

It is off until you set up a client. Until then `/mcp` answers `404` to
everything.

## Set up a client

1. Open **Settings, Integrations**. The **MCP server** card is below the VPN card.
2. Choose **Add client** and pick your assistant. A client that is not listed
   connects through **Other client**.
3. Give the token a name and choose its rights: **Read only**, **Add and read**
   or **Read, add and control**.
4. Choose **Connect**. KnightLoader makes a token for this client and shows it
   once, with the configuration for exactly that client, where the
   configuration lives, and how the client can read the token without keeping
   it in the configuration file where it can.
5. Start the client. The window turns green on its first call.

Each client gets a token of its own, and the token also shows in the list of
API tokens. Only a token made here gets into `/mcp`: the token you typed into
Sonarr does not, whatever its rights. **Remove** on a client's row revokes its
token at once. Setting, changing or removing the password revokes every token,
the clients' included; their rows then say **Token revoked**, and you add them
again.

## What an assistant can do {#tools}

| Tool | What it does | Right |
|---|---|---|
| `get_status` | Whether the queue runs, what is in flight, the speed and the limits in force, quiet mode and whether a stream on a media server holds it, what is still owed, how many links wait in the collector, and the rights of this token | read |
| `list_downloads` | The download list without the collector, filterable by state, package and text | read |
| `list_collector` | The links in the link collector, grouped by package | read |
| `list_history` | Finished downloads, newest first | read |
| `list_instances` | The other KnightLoader instances this one knows | read |
| `add_links` | Puts links, magnets or text with links in it into the collector | add |
| `add_container` | Opens a DLC, RSDF, CCF or text list and puts its links into the collector | add |
| `start_collector` | Moves collected links, whole packages or the whole collector into the queue | control |
| `pause_downloads` | Pauses downloads by id | control |
| `resume_downloads` | Puts paused downloads back into the queue | control |
| `remove_downloads` | Removes entries from the list; files on disk stay | control |
| `set_queue` | Runs or halts the queue and switches quiet mode | control |
| `stop_all` | Stops every transfer now and halts the queue | control |

Every client sees the whole list, and a call to a tool its token has no right
for is refused with the right it lacks. The rights decide on the next call, so
a client does not have to reconnect.

These stay in the web interface: settings, accounts, passwords, tokens, the
speed limits themselves, deleting downloaded files, `.nzb` files and containers
only the JDownloader backend can open. Whatever an assistant reads goes to the
AI provider behind it, so the answers never carry the passwords or credentials
a link may hold, whatever the token's rights.

## Clients in the cloud {#cloud}

Grok and Le Chat call KnightLoader from their vendors' servers, so KnightLoader
has to be reachable from the internet over https with a trusted certificate,
for example behind a reverse proxy. A login on the proxy may guard the web
interface, but `/mcp` has to pass through to KnightLoader, which checks the
token itself. ChatGPT and Claude on claude.ai sign in through OAuth only, which
KnightLoader does not offer.

## Other clients {#other-clients}

Any client that speaks Streamable HTTP works:

- URL: the address of the web interface plus `/mcp`, for example
  `http://192.168.1.10:8080/mcp`, or `https://example.com/kl/mcp` behind a
  proxy that mounts KnightLoader under a path.
- The token in `Authorization: Bearer <token>` or in `X-API-Key: <token>`. If
  both are sent they must carry the same token.
- `POST` with `Content-Type: application/json` and
  `Accept: application/json, text/event-stream`, one JSON-RPC message per
  request; batches are refused.

## Troubleshooting {#troubleshooting}

| What you see | What it means |
|---|---|
| `404` | No client is set up, or the path is wrong. The endpoint is `/mcp`. |
| `401` | The token is missing, mistyped or revoked, or it was not made for an MCP client. A proxy may be dropping the `Authorization` header; try `X-API-Key`. |
| `403` | The request came from a browser page on another site. |
| `421` | No password is set and the client reached KnightLoader under a domain it does not know. Add the domain under **Settings, Pairing, Known domains**, or set a password. |
| `not_permitted` | The token lacks the right the tool needs. Remove the client and add it again with that right. |
