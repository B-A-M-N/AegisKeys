# AegisKeys Credential Broker Protocol

The broker is an explicit, local-only way for an authorized application to
resolve a named credential from AegisKeys. Applications use stable binding
names instead of storing copies of API keys. After the credential is rotated
in AegisKeys, the next resolve returns the new value.

This is a language-neutral Unix-domain socket protocol. No AegisKeys package,
module, SDK, or generated client is required.

## Trust boundary

Read this before enabling a grant:

> AegisKeys protects a credential before and during controlled delivery. Once
> an authorized client receives the raw credential, that client can copy,
> cache, log, transmit, or misuse it. AegisKeys cannot revoke a credential the
> client already learned; rotate the credential at its provider.

The socket is local-only. There is no TCP listener. Linux peer credentials and
executable path/hash pinning restrict accidental cross-application exposure;
they are **not** a sandbox and do not defend against malware already running as
the same user. A grant to Python, Node.js, Ruby, or another shared interpreter
authorizes every script run by that interpreter. AegisKeys refuses such grants
unless `--allow-interpreter-wide` is explicit and labels them interpreter-wide.
Script arguments are not a security boundary. Use a dedicated launcher, OS-user
separation, or a sandbox when per-application isolation is required.

## Runtime files

```text
~/.config/aegiskeys/broker.json       metadata only, 0600
~/.config/aegiskeys/run/              0700
~/.config/aegiskeys/run/broker.sock   Unix socket, 0600
```

`broker.json` contains binding and grant metadata. It never contains secret
values. Rotation request bodies are never persisted or audited.

## Transport

HTTP/1.1 over a Unix-domain socket:

- request bodies: `application/json`
- response bodies: `application/json`
- no cookies, redirects, routing headers, or query strings carrying credentials
- maximum request body: 64 KiB
- read/write timeout: 5 seconds
- idle timeout: 30 seconds
- bounded concurrent requests

Protocol version is fixed at `v1`.

## Operations

### `GET /v1/status`

Response:

```json
{"protocol":"v1","locked":false}
```

### `POST /v1/resolve`

Request:

```json
{"binding":"athena/openrouter"}
```

Single-component response:

```json
{
  "binding":"athena/openrouter",
  "kind":"api_key",
  "env_var":"OPENROUTER_API_KEY",
  "value":"..."
}
```

Multi-component response:

```json
{
  "binding":"service/aws",
  "kind":"api_key",
  "components":{
    "AWS_ACCESS_KEY_ID":"...",
    "AWS_SECRET_ACCESS_KEY":"..."
  }
}
```

Treat returned values as short-lived runtime material. Do not log, cache beyond
provider-client lifetime, or write them to files.

### `POST /v1/rotate`

Requires an explicit `rotate` grant and a secret policy that independently
permits broker rotation.

Request:

```json
{"binding":"infrastructure/cloudflare","value":"..."}
```

Success response is `{}`. The operation changes only the target secret value
and its rotation timestamps. It cannot change labels, providers, policies,
bindings, grants, unrelated records, or any file path.

## Errors

Errors are generic and never echo request values:

| HTTP | Meaning |
|------|---------|
| 400 | malformed, oversized, unknown-field, or invalid request |
| 403 | executable, binding, or secret policy denied access |
| 404 | binding unavailable |
| 405 | method not allowed |
| 415 | media type not `application/json` |
| 423 | broker vault is locked |

## Consumer examples

### Python

```python
import http.client, json, socket

class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost")
        self.path = path
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.path)

conn = UnixConnection("/home/user/.config/aegiskeys/run/broker.sock")
conn.request("POST", "/v1/resolve", body=json.dumps({"binding":"athena/openrouter"}), headers={"Content-Type":"application/json"})
result = json.load(conn.getresponse())
print(result["env_var"])
```

### Node.js

```javascript
import http from 'node:http';

const options = {
  socketPath: '/home/user/.config/aegiskeys/run/broker.sock',
  path: '/v1/resolve',
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
};
const req = http.request(options, res => {
  let body = '';
  res.setEncoding('utf8');
  res.on('data', chunk => { body += chunk; });
  res.on('end', () => {
    if (res.statusCode !== 200) throw new Error(`broker resolve failed: ${res.statusCode}`);
    const credential = JSON.parse(body);
    // Configure the intended in-memory client; never print or persist value.
    process.env[credential.env_var] = credential.value;
  });
});
req.on('error', console.error);
req.end(JSON.stringify({binding: 'athena/openrouter'}));
```

### Go

```go
client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
    var d net.Dialer
    return d.DialContext(ctx, "unix", socketPath)
}}}
```

### Rust

A standard HTTP client with a Unix-socket connector can call the same paths;
do not add an AegisKeys dependency solely for this protocol.

### curl

```sh
curl --unix-socket "$HOME/.config/aegiskeys/run/broker.sock" \
  -H 'Content-Type: application/json' \
  -d '{"binding":"athena/openrouter"}' \
  http://broker/v1/resolve
```

## Administration

```sh
aegiskeys broker serve
aegiskeys broker status
aegiskeys access binding add --name athena/openrouter --secret key_...
aegiskeys access binding list
aegiskeys access binding inspect --binding athena/openrouter
aegiskeys access binding rebind --binding athena/openrouter --secret key_...
aegiskeys access binding delete --binding athena/openrouter
aegiskeys access grant --binding athena/openrouter --exec /path/to/athena --capability resolve
aegiskeys access list
aegiskeys access inspect --grant grant_...
aegiskeys access revoke --grant grant_...
```

Grant creation displays the executable, binding, masked target, operations,
and expiration, then requires interactive confirmation. Rotate grants require
acknowledgment of the stronger operation before confirmation.

`access binding add --allow-broker-resolve` and/or
`--allow-broker-rotate` can enable the matching record policy after displaying
the target and requiring the word `ENABLE`. Omit both flags to create binding
metadata without changing secret policy. Existing and migrated records default
to broker-deny. Grant creation itself does not silently enable secret policy.

Existing `run`, `env`, `handoff`, `vault reveal`, `vault copy`, and `vault env`
surfaces remain separate. The broker does not offer generic file writing or
secret enumeration.
