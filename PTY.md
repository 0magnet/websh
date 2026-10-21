# The pty protocol

A terminal on another machine, over one WebSocket. websh's `pty` applet is a
client of it (shell/pty), `pty.Handler` is a server of it over a local
pseudo-terminal, and `websh serve` puts that server and websh's terminal page
(package term) on one port. Skywire's visors speak it too.

## Messages

Binary messages are terminal bytes: the keys the person types, client to
server, and the program's output, server to client.

Text messages are control, one JSON object each, with a `"type"`:

| type | from | fields | meaning |
|---|---|---|---|
| `resize` | client | `cols`, `rows` | the terminal's size; sent on open and whenever it changes |
| `session` | server | `id` | the shell outlives this connection, and `id` reattaches it |

A text message that is not a JSON object with a `"type"` is output, for a
server that sends text that way. A side ignores a type it does not know.

## Reconnecting

A server that keeps a shell running after its connection drops sends a
`session` message once it has started it. A client that loses the connection
opens a new one to the same address with `?sid=<id>` added, and the server
attaches that shell, replaying what it wrote meanwhile. A server without this
never sends `session`, and a client then ends when the connection does.

A server ends a session by closing with code 1000, as when the shell exits,
and the client does not reconnect after that. Any other close is a dropped
connection, such as either end restarting, and the client keeps trying, waiting
up to 10 seconds between attempts. A server that no longer has the shell
starts a new one.

## Origin

A page on another site can open a WebSocket to any address it likes; CORS does
not apply. A server must refuse a connection whose `Origin` is not its own (or
one it explicitly allows), or any website its user visits can drive the shell.
`pty.Handler` does, through coder/websocket's origin check.

## The applet

    pty <address>

The address is `ws://`, `wss://`, `http(s)://` or a path, resolved against the
page's address (`document.baseURI`), so a page mounted under a prefix works. At
the start of a line, `~.` ends the session and `~~` sends one `~`, as in ssh.
What arrives is marked as a remote program's for the progressive terminal
protocol (PROTOCOL.md, Trust).
