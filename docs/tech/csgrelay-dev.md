# CSGRelay development relay

The opt-in CSGRelay panel publishes local agents and invokes discovered agents through the external CSGRelay service.
The execution adapter uses the public CSGRelay WebSocket SDK and the existing Agent Engine conversation boundary.
It contains no NATS client and never connects directly to a different CSGClaw server.

## Setup

The development SDK dependency resolves to `../csgrelay/sdk/go` through the temporary local replacement in `go.mod`.
Clone the private [CSGRelay repository](https://git-devops.opencsg.com/product/agentichub/csgrelay) into `../csgrelay` beside this checkout and start its service and NATS using that repository's README.

Set these variables in each CSGClaw server process:

```sh
export CSGCLAW_CSGRELAY_DEV_PANEL=1
export CSGCLAW_CSGRELAY_URL=http://127.0.0.1:28080
export CSGCLAW_CSGRELAY_TOKEN='<token configured in CSGRelay>'
```

Use different CSGRelay user/client tokens for the publishing and calling servers.
For a second instance, also set `CSGCLAW_DATA_DIR` to a separate application data directory and pass its own `--config` file with a different listen address.
Keep writable local registry paths inside that instance's data directory too.
Changing only the port or config path does not isolate application state.

Open `/csgrelay-dev` on each server.
When personal-service authentication is enabled, enter that local server's access token under the panel's local credential settings.
This credential is separate from the upstream CSGRelay token, which stays server-side.

## Publishing and calling

On the publishing server, select a running local agent, choose its display name and visibility, and publish it.
The backend saves the remote/local ID binding and starts an outbound WebSocket execution connection.
Bindings are restored on server restart; credentials come from the process environment and are not saved with the bindings.

On the calling server, choose the remote agent and submit a text request.
The panel supports Responses JSON or SSE, conversation continuation, cancellation, relay trace inspection, and periodic directory refresh.
Only the publishing server shows the execution connection's disconnect and reconnect controls.

Execution uses `Agent Engine -> Runtime Adapter`, preserving native tool execution and cancellation.
The relay Response ID becomes the Engine turn ID for cross-system tracing.
CSGRelay session IDs map to isolated, persisted native conversation bindings.
The model provider proxy `/llm/responses` is not used to simulate agent execution.

The route and proxy API are disabled unless the explicit development flag, CSGRelay URL, and server-side CSGRelay token are all present.
This panel and local SDK replacement are temporary integration surfaces, not production UI onboarding.
