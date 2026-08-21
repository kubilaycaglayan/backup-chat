Create a very small, lightweight web chat application that I can self-host on an Ubuntu Server.

The goal is simplicity. This is a backup chat room intended for two people. Do not overengineer it.

## Technology

Use:

* Go for the backend.
* Go's standard library wherever practical.
* WebSockets for real-time messaging.
* Plain HTML, CSS, and vanilla JavaScript for the frontend.
* No React, Vue, npm, Node.js, frontend build system, or CSS framework.
* No database.
* Store messages in a local JSONL file.
* The finished application should compile into a single Go executable.

Keep external Go dependencies to an absolute minimum. Use one well-maintained WebSocket library if necessary.

## User flow

When someone opens:

`http://SERVER_IP:PORT`

show a very simple nickname prompt.

The user enters a nickname and enters the chat.

There are:

* no accounts
* no registration
* no passwords
* no login system
* no email addresses
* no profiles
* no rooms
* no private messages
* no attachments
* no reactions
* no editing/deleting messages
* no unnecessary features

This is just one shared chat.

## Chat interface

Create a minimal responsive interface containing:

* message history
* nickname of each sender
* timestamp of each message
* text input
* Send button

Pressing Enter should send the message.

The interface should work properly on both desktop and mobile browsers.

Keep the design extremely simple and lightweight.

Messages from the current user should be visually distinguishable from messages from the other person, but avoid unnecessary UI complexity.

When a new message arrives, automatically scroll to the bottom unless the user has intentionally scrolled upward to read older messages.

## Real-time communication

Use WebSockets.

When one user sends a message:

1. Receive it on the server.
2. Validate it.
3. Generate the timestamp on the server.
4. Write it to disk.
5. Broadcast it to all connected WebSocket clients.

Do not trust timestamps supplied by clients.

Use UTC internally.

The browser can display timestamps in the user's local timezone.

## Message format

Store one JSON object per line, for example:

```json
{"timestamp":"2026-08-21T16:45:23Z","nickname":"Kubilay","message":"Hello"}
```

Use a file such as:

```text
data/messages.jsonl
```

Create the directory/file automatically when necessary.

## Message history

When someone connects, load the existing messages and send the retained history to that client.

Messages older than 30 days do not need to be retained.

Implement automatic cleanup:

* clean expired messages when the application starts
* perform cleanup periodically while the server is running, for example once per day

When compacting the JSONL file, rewrite it safely using a temporary file and atomic rename so that an interrupted write does not destroy the chat history.

The server must synchronize file access correctly so concurrent WebSocket messages cannot corrupt the file.

Do not load an unlimited amount of historical data into memory unnecessarily.

## Validation and security

Even though this deliberately has no authentication, assume the HTTP port will be exposed to the public internet.

Implement basic defensive measures without turning this into a complicated security system.

Requirements:

* Treat all chat messages as plain text.
* Never render received messages with `innerHTML`.
* Use `textContent` or equivalent safe DOM operations.
* Reject empty messages.
* Trim nicknames.
* Limit nickname length to approximately 32 characters.
* Limit message length to approximately 2,000 characters.
* Reject malformed WebSocket messages.
* Add a simple per-IP or per-connection rate limit to prevent obvious flooding.
* Apply reasonable HTTP server timeouts.
* Limit request/header sizes where appropriate.
* Do not expose arbitrary filesystem paths.
* Do not allow the client to specify the message storage path.
* Do not add analytics or third-party JavaScript.
* Do not use external CDNs.

Since the frontend and WebSocket server come from the same Go application, keep everything same-origin.

Do not implement authentication unless I explicitly ask for it later.

## Configuration

Support configuration using environment variables.

At minimum:

```text
PORT=50000
DATA_FILE=./data/messages.jsonl
RETENTION_DAYS=30
```

Use sensible defaults when these variables are absent.

Listen on all interfaces so the service can be reached remotely.

For example:

```text
0.0.0.0:50000
```

## Project structure

Keep the repository small.

Something approximately like this is enough:

```text
backup-chat/
├── go.mod
├── main.go
├── web/
│   ├── index.html
│   ├── app.js
│   └── style.css
├── data/
│   └── .gitkeep
├── backup-chat.service
├── .gitignore
└── README.md
```

Feel free to split `main.go` into a few small Go files if that substantially improves readability, but do not create unnecessary abstraction layers.

Embed the static frontend files into the Go executable using Go's `embed` package so deployment does not require copying frontend assets separately.

Do not embed the message data file.

## Ubuntu deployment

This will run directly on Ubuntu Server.

Do not require Docker.

Provide a systemd unit file called:

```text
backup-chat.service
```

It should:

* run the compiled executable
* restart automatically after failure
* restart automatically after reboot
* use a dedicated working directory
* write normal application logs to stdout/stderr so they are available through journalctl

Document commands such as:

```bash
go build -o backup-chat
./backup-chat
```

and systemd installation/startup commands in the README.

Also document that the configured TCP port must be allowed through the Ubuntu firewall/router/cloud firewall when applicable.

The README should show that the application can then be reached using:

```text
http://SERVER_PUBLIC_IP:50000
```

Do not assume that I have a domain name.

## Code quality

Prioritize boring, readable, conventional code.

Avoid:

* unnecessary architecture
* interfaces with only one implementation
* dependency injection frameworks
* ORM
* database abstraction
* frontend framework
* CSS framework
* Docker
* Kubernetes
* microservices
* message queues
* user management
* complex configuration libraries

Use descriptive names.

Keep functions reasonably small.

Handle errors explicitly.

Use graceful shutdown so active connections and pending file writes are handled cleanly when systemd stops the process.

## Tests

Add focused Go tests for the important server-side behavior, especially:

* message validation
* nickname validation
* JSONL serialization/deserialization
* 30-day retention filtering

Do not create a huge test suite for such a small project.

Run:

```bash
go test ./...
```

and:

```bash
go build ./...
```

before considering the implementation finished.

## Final result

Implement the application completely in the repository.

After implementation:

1. Show me the resulting file structure.
2. Explain the important design choices briefly.
3. Give me the exact commands to build it.
4. Give me the exact commands to install and start the systemd service.
5. Give me the exact command needed to allow the configured port with UFW.
6. Tell me how to inspect its logs with `journalctl`.
7. Tell me which files contain the persistent messages.
8. Do not add features beyond the requirements above unless they are necessary for correctness or basic security.
