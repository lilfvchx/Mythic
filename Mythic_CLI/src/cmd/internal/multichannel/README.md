# Multi-channel communication scaffold (MVP)

This package provides an initial, pluggable communication layer for a research agent workflow:

- A common `Transport` interface across channels.
- A `CommunicationManager` with priority ordering and automatic failover.
- Threshold-based payload offloading via `UploadBlob` + `BlobRef`.
- A local task store abstraction (`LocalTaskStore`) with an in-memory implementation.

## Current scope

This is an initial foundation only. Concrete providers (Slack, GitHub, Dropbox, Mega, Pastes.io), crypto primitives, telemetry exporters, and durable persistence backends should be implemented in follow-up phases.
