# Multi-channel communication scaffold (MVP)

This package provides an initial, pluggable communication layer for a research agent workflow:

- A common `Transport` interface across channels.
- A `CommunicationManager` with priority ordering and automatic failover.
- Threshold-based payload offloading via `UploadBlob` + `BlobRef`.
- A local task store abstraction (`LocalTaskStore`) with an in-memory implementation.

## Implemented transports

- `SlackTransport` (`slack.go`): control messages via channel messages, acknowledgements via reactions, blob offload via Slack file upload API.
- `MegaTransport` (`mega.go`): tasks/results persisted as JSON files and blobs uploaded directly in per-agent folder hierarchy.
- `PastesTransport` (`pastes.go`): low-bandwidth emergency channel for small text payloads only; blob offload is intentionally unsupported.

## Configuration assumptions

- Slack:
  - Use bot token and channel ID (recommended via environment variables like `MULTICHANNEL_SLACK_TOKEN` / `MULTICHANNEL_SLACK_CHANNEL`).
- Mega:
  - Use account email/password (recommended via `MULTICHANNEL_MEGA_EMAIL` / `MULTICHANNEL_MEGA_PASSWORD`) and optional base folder (`MULTICHANNEL_MEGA_BASE`).
- Pastes.io:
  - Default base URL `https://pastes.io` (override for test environments).

No credentials are hardcoded; constructors expect values supplied by caller configuration.

## Wiring into `CommunicationManager`

```go
// example: prioritize Slack -> Mega -> Pastes fallback
mgr := NewCommunicationManager(
    agentID,
    NewMemoryTaskStore(),
    ManagerConfig{FailureThreshold: 3, Cooldown: 30 * time.Second, BlobThreshold: 256 * 1024},
    slackTransport,
    megaTransport,
    pastesTransport,
)
```

Order passed to `NewCommunicationManager` is the priority order used for polling and failover.

## Minimal usage example

```go
// NOTE: keep tokens/credentials in env/config management, not source code.
slackTransport, _ := NewSlackTransport(os.Getenv("MULTICHANNEL_SLACK_TOKEN"), os.Getenv("MULTICHANNEL_SLACK_CHANNEL"))
megaTransport, _ := NewMegaTransport(os.Getenv("MULTICHANNEL_MEGA_EMAIL"), os.Getenv("MULTICHANNEL_MEGA_PASSWORD"), os.Getenv("MULTICHANNEL_MEGA_BASE"))
pastesTransport, _ := NewPastesTransport("")

mgr := NewCommunicationManager(agentID, NewMemoryTaskStore(), ManagerConfig{}, slackTransport, megaTransport, pastesTransport)
_ = mgr.Register(context.Background(), map[string]string{"profile": "research"})
```

## Known limitations

- Slack `DownloadBlob` currently returns an explicit error because fetching `URLPrivateDownload` requires authenticated HTTP GET orchestration beyond the SDK helper method in this package.
- Pastes is intentionally constrained and should only carry small control traffic.
- Mega file enumeration semantics depend on account/folder size; this implementation is optimized for per-agent folder isolation.
