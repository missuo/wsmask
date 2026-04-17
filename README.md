# wsmask

Tiny transparent TCP → WebSocket tunnel with HTTP `Host:` header spoofing.
Built for OpenWrt. No encryption, no protocol — just `io.CopyBuffer` in a
WebSocket frame.

## What it does

1. OpenWrt router hijacks LAN TCP traffic via nftables `REDIRECT`.
2. `wsmask-client` extracts the real destination from `SO_ORIGINAL_DST`.
3. Opens a WebSocket to the VPS with `Host: <fake-host>` + `X-Target: <real>` + `X-Auth: <token>`.
4. `wsmask-server` on the VPS validates the token, dials the real target, and byte-pumps both directions.

## What it is not

- **Not** a DPI-circumvention tool. Payloads are plaintext. Host spoofing only
  fools middleboxes that look at the HTTP `Host:` header on plain HTTP/WS.
- **Not** for hostile networks (GFW-class). Use `sing-box`, `xray`, `mihomo`.
- **No UDP**, no QUIC, no IPv6. TCP-only.
- **No encryption**. End-to-end TLS (e.g. HTTPS target sites) is preserved
  because wsmask doesn't touch the tunneled bytes.

## Architecture

```
LAN device
    │ plain TCP
    ▼
OpenWrt nftables (REDIRECT)  ──▶  127.0.0.1:12345
                                         │
                                         ▼
                                   wsmask-client
                                         │ ws:// + fake Host + X-Target + X-Auth
                                         ▼
                                       VPS:8080
                                         │
                                    wsmask-server
                                         │  net.Dial(X-Target)
                                         ▼
                                    real target
```

## Build

```sh
# OpenWrt client (AW1000 / IPQ8072A → arm64)
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -trimpath \
  -o dist/wsmask-client-linux-arm64 ./cmd/wsmask-client

# VPS server (amd64)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath \
  -o dist/wsmask-server-linux-amd64 ./cmd/wsmask-server
```

Common router targets: `arm64`, `arm`+`GOARM=7`, `mipsle`+`GOMIPS=softfloat`.

## VPS

```sh
./wsmask-server -listen :8080 -auth 'change-me'
```

Default WS path is `/ec-McAuth`. Everything else returns a decoy `nginx 404`.

Open your cloud firewall for TCP 8080. Wrap in systemd for production.

## OpenWrt

```sh
# 1. Upload files
scp dist/wsmask-client-linux-arm64      root@ROUTER:/usr/bin/wsmask-client
scp deploy/openwrt/99-wsmask.nft        root@ROUTER:/etc/nftables.d/

# 2. On the router: edit VPS_IP and LAN_IFACE
vi /etc/nftables.d/99-wsmask.nft

# 3. Apply rules
fw4 restart
nft list table ip wsmask

# 4. Run the client
/usr/bin/wsmask-client \
  -listen 127.0.0.1:12345 \
  -server ws://VPS_IP:8080/ec-McAuth \
  -fake-host www.bing.com \
  -auth 'change-me'
```

Verify from a LAN device: `curl http://ifconfig.me` should return the VPS's
public IP; `wsmask-server` logs should show `Host="www.bing.com"`.

## Dev mode (no nftables / macOS)

Pass `-target host:port` to bypass `SO_ORIGINAL_DST` and forward every
accepted connection to a fixed upstream:

```sh
./wsmask-client -listen 127.0.0.1:12345 \
  -server ws://VPS:8080/ec-McAuth \
  -fake-host www.bing.com \
  -auth 'change-me' \
  -target ifconfig.me:80

curl -H 'Host: ifconfig.me' http://127.0.0.1:12345/
```

## Flags

**`wsmask-client`**

| Flag | Default | Description |
|---|---|---|
| `-listen` | `127.0.0.1:12345` | Local TCP listen address |
| `-server` | *(required)* | Upstream `ws://host:port/path` |
| `-fake-host` | `www.bing.com` | HTTP `Host` header sent on WS handshake |
| `-auth` | *(required)* | Shared auth token |
| `-target` | *(empty)* | Dev mode: skip `SO_ORIGINAL_DST`, force this target |

**`wsmask-server`**

| Flag | Default | Description |
|---|---|---|
| `-listen` | `:8080` | HTTP listen address |
| `-path` | `/ec-McAuth` | WebSocket upgrade path |
| `-auth` | *(required)* | Shared auth token |
| `-dial-timeout` | `10s` | Upstream dial timeout |

## Wire format

The WS handshake carries the real destination and the shared auth token in a
single URL query parameter:

```
GET /ec-McAuth?c=<base64url(target|token)> HTTP/1.1
Host: <fake-host>
Upgrade: websocket
User-Agent: Mozilla/5.0 ... Chrome/131.0.0.0 Safari/537.36
```

No custom `X-*` headers are sent — the handshake looks like a plain
authenticated WS subscription request from a Chrome browser.

## Security notes

- The server is a semi-open TCP proxy gated only by the shared token in `?c=`.
  Treat it like an SSH key — rotate regularly, don't commit it.
- Private / loopback / link-local / multicast destinations are rejected
  server-side to prevent SSRF into the VPS's internal network.
- Non-WS requests and auth failures return a decoy `nginx 404` page.

## License

MIT
