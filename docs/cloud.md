# Accounts and the relay (design spike)

[Docs](README.md)

This is the spec for the first phase of Sticky Sessions: machines sign in to
an account on a relay, reach each other through it with no SSH and no
inbound ports, and the relay never sees what they say to each other. It
describes what is built so far. For how to
use it, see [More machines](machines.md#through-the-relay-accounts).

Status: working code, tested on Linux, not reviewed by anyone outside the
project yet. The PRD gates the public beta on an external audit of the
encryption; nothing here replaces that.

## What is built

| PRD | Here |
|---|---|
| R2 `gecko login`, `logout`, `whoami`, per-machine keys `0600` | Built. Accounts live on the relay (no OIDC yet, see [Identity](#identity)). |
| R3 outbound WebSocket, backoff, `HTTPS_PROXY` | Built. `wss://` only, plain `ws://` only to a relay on this machine. |
| R4 relay forwards Gecko's protocol unchanged | Built. A relayed machine is a `hub.Remote` like an SSH one; only the dial function differs. |
| R5 end-to-end encryption | Built. Noise IK between the two daemons. |
| R6 approval from an existing machine | Built. `gecko approve <code>` or the prompt in the app. |
| R7 revoke, links close within 5 s | Built. `gecko revoke <name>` or Revoke in Devices. |
| R8 fleet view | Built. Machines grouped Local, SSH, Cloud, deduplicated by machine id. |
| R9 nothing changes unless signed in | Built. See [Signed out means off](#signed-out-means-off). |
| R16 self-hostable relay | Started: `gecko relay`, one JSON file, no database. |

Out of scope for this branch: real OIDC sign-in (Google, GitHub, Apple),
Web Push (R10), the phone PWA, sticky context (R11), settings sync over the
account (R12), the direct peer-to-peer path (R13), passkeys (R14), the audit
log (R15), a hosted relay, and Noise in the browser. A browser still talks to
its own daemon with the local token, and that daemon does the relaying.

## Pieces

```
 laptop daemon                     relay                      gpu-box daemon
 ─────────────                     ─────                      ──────────────
 hub ── Remote "gpu-box"                                   server.ServePeer
          │ dial: relay + Noise                                    ▲
          ▼                                                        │
 cloud.Agent ── control WS ──►  accounts, device lists  ◄── control WS ── cloud.Agent
              ── /v1/dial WS ─► pairs the two sockets ◄── /v1/accept WS ─┘
                 (Noise ciphertext both ways; the relay has no key)
```

- `internal/cloud`: keys, the signed device list, `gecko login`, the Noise
  link (`SecureConn`, a `proto.Conn`), and the `Agent` a signed-in daemon
  runs.
- `internal/relay`: `gecko relay`.
- `internal/hub`: `Remote` takes a `DialFunc`. SSH (`SSHDial`) is one, the
  relay is another. `SetCloudMachines` turns the account's machines into
  Remotes and merges a machine that is also an SSH host.

## Keys

Each machine generates two key pairs the first time it signs in, in
`~/.gecko-terminal/cloud/key.json` (or `$GECKO_HOME/cloud/`), mode `0600` in
a `0700` directory:

- **X25519** for Noise. The static key that identifies the machine in a
  link.
- **Ed25519** for signing device lists and relay requests. The machine's id
  is the first 80 bits of SHA-256 of this public key, in base32 (16
  characters).

Private keys never leave the machine. `gecko logout` deletes them; signing
in again makes new ones.

`account.json` next to it holds the relay URL, the account name, this
machine's id, the account fingerprint, and the latest device list this
machine has verified.

## The device list

An account is defined by a chain of device lists. Each version lists the
trusted devices:

```json
{
  "account": "roy", "version": 3,
  "devices": [
    { "id": "…", "name": "laptop", "os": "linux", "kind": "machine",
      "noise": "<X25519 public>", "sign": "<Ed25519 public>", "added": 1760000000000 },
    { "id": "…", "name": "recovery code", "kind": "recovery", "sign": "<Ed25519 public>" },
    { "id": "…", "name": "gpu-box", "kind": "machine", "noise": "…", "sign": "…", "by": "<laptop's id>" }
  ],
  "signer": "<laptop's id>", "sig": "<Ed25519 signature>"
}
```

Rules, checked by every daemon (`cloud.Verify`) and by the relay:

- Version 1 is signed by a device in it (the first machine).
- Version n+1 must be signed by a device listed in version n. That device
  can be a machine or the recovery code.
- Every id matches its key, and every machine has a 32-byte Noise key.
- A daemon only moves forward: it ignores versions at or below the one it
  has, and stops at the first version that doesn't verify.

The signature covers `"gecko-device-list-v1\n"` followed by the list as
JSON with `sig` empty, in Go's field order. A later version should switch to
a canonical encoding that other languages can reproduce.

The relay stores the whole chain so a daemon that was offline can catch up
version by version. A machine joining for the first time verifies the
chain from version 1.

The **account fingerprint** is a hash of version 1, shown as
`XXXX-XXXX-XXXX` by `gecko whoami` and in Devices. It is the same on every
machine of the account.

**Why a signed list rather than approvals pushed peer to peer:** a daemon
that was offline during an approval or a revocation still gets it, from the
relay, and still can't be lied to by the relay, since it checks every step
against its own copy. The cost is that a compromised trusted machine can
sign any list (see the threat table).

## Relay requests

There are no bearer tokens. Every HTTP request and WebSocket upgrade carries

```
Authorization: Gecko id=<device id>,ts=<unix seconds>,sig=<base64url Ed25519>
```

over `"gecko-relay-v1\n" + method + "\n" + path?query + "\n" + ts + "\n" +
hex(sha256(body))`. The relay accepts it for two minutes and only from a
device in the account's latest list. So a credential lives two minutes, is
bound to the device's key, and stops working the moment a list without the
device is published. Within those two minutes a captured request could be
replayed (TLS is what prevents capture).

## Relay API

| Call | Who | What |
|---|---|---|
| `POST /v1/enroll` | a new machine, signed with its own key | `{account, device}`: `new` (no such account), `pending` + code + expiry, or `approved` |
| `GET /v1/enroll` | the waiting machine | `pending`, `approved` or `gone` |
| `POST /v1/account` | the first machine | `{list}`: version 1 of a new account |
| `GET /v1/chain?since=n` | a listed device | the list versions after n |
| `POST /v1/list` | a listed device | `{list}`: the next version (approve, revoke, sign out); 409 if someone else published first |
| `GET /v1/pending`, `POST /v1/deny` | a listed machine | machines waiting for approval |
| `GET /v1/connect?since=n` | a listed machine | the control WebSocket |
| `GET /v1/dial?to=<id>` | a listed machine | a link to another machine of the account (404 if it is offline) |
| `GET /v1/accept?session=<id>` | the target | its end of that link |

On the control socket the relay sends JSON: `list` (new versions), `presence`
(which machines are online, when each was last seen and from what address),
`pending` (machines waiting) and `incoming` (a pairing id to accept). The
relay pings every 25 s; a daemon reconnects with exponential backoff from 1 s
to 30 s with jitter.

State is `relay.json` in the data directory (`--data`, default
`~/.gecko-terminal/relay`): accounts, their chains, pending enrollments and
last-seen times. It is rewritten on every change. Fine for a person or a
team; a hosted relay would need a real store.

## A link between two machines

1. The laptop's hub wants gpu-box. It opens `/v1/dial?to=<gpu-box id>`.
2. The relay checks both are machines in the same account and gpu-box is
   connected, then sends gpu-box `incoming` with a pairing id.
3. gpu-box opens `/v1/accept?session=<id>`. The relay pipes the two
   WebSockets together and from then on only copies binary frames.
4. Noise handshake, `Noise_IK_25519_ChaChaPoly_BLAKE2s`, prologue
   `gecko-cloud-v1`. The laptop is the initiator and takes gpu-box's static
   key from its own verified list, never from the relay. gpu-box reads the
   laptop's static key from the first message and looks it up in its own
   list. If it isn't there, gpu-box closes the socket without answering.
5. Inside the session runs Gecko's normal protocol, exactly what
   `gecko bridge` carries over SSH: the laptop sends `hello` with scope
   `local`, then lists, attaches, input and output. Since protocol 10 the
   local scope is gpu-box's decision, not the laptop's: gpu-box serves every
   relay peer with `server.ServePeer`, which grants the local scope whatever
   `hello` asks for. A peer gets gpu-box's own tabs and nothing else: not
   the machines gpu-box reaches over SSH or links, not its machine list,
   settings or account (approve, revoke). See [the anchor](anchor.md#security-model). Each protocol message is
   one or more Noise messages of `flags | payload` (flag 1: binary frame,
   flag 2: more chunks follow), at most 65,000 bytes of plaintext each.

The hub reconnects a dropped link with the same backoff as SSH and resumes
every stream from its last byte. When the relay reports a machine online,
its Remote retries at once.

## Enrollment and approval

**First machine.** `gecko login --relay URL --account roy`. The relay says
the account doesn't exist; the machine creates version 1 with itself and a
recovery device, signs it, and posts it. It prints the account fingerprint
and the recovery code once.

**Every later machine.**

1. `gecko login` on gpu-box posts its device entry. The relay records it as
   pending for 10 minutes, with the address it came from, and pushes it to
   every connected machine of the account.
2. gpu-box prints its code, for example `ANHI-H4TC`. The code is 40 bits of
   SHA-256 over gpu-box's Noise and signing public keys, so gpu-box computes
   it itself.
3. Every signed-in machine shows "gpu-box (Linux, 203.0.113.4) wants to join.
   Approve?". To approve, you type the code shown on gpu-box (in the app) or
   run `gecko approve ANHI-H4TC`. The approving machine computes the code from
   the keys the relay handed it and only approves when they match. A relay
   that swapped in its own keys would produce a different code.
4. The approving machine signs version n+1 with gpu-box added and posts it.
   The relay pushes it to every machine; each verifies it and adds gpu-box
   to its sidebar.
5. gpu-box, polling every 2 s, sees it was approved, downloads the chain,
   verifies it from version 1, checks it is listed with its own keys, and
   saves it. It prints the account fingerprint and which machine approved
   it. With `--fingerprint XXXX-XXXX-XXXX` (from `gecko whoami` on a
   machine already in the account) it refuses an account with a different
   fingerprint.

At most 5 machines can wait per account. **Deny** drops a waiting machine.

**Recovery code.** 100 random bits shown once, as `XXXX-XXXX-XXXX-XXXX-XXXX`.
It is the seed of an Ed25519 key whose public half is in the list as the
`recovery` device. `gecko login --recovery CODE` signs the next version with
it, adding the machine with no approval. The relay never sees the code. The
recovery entry can't be revoked from the app (lose it and you lose this
way back in; a later version should let you rotate it).

## Revocation

`gecko revoke gpu-box` (or **Revoke** in Devices, or right-click the machine)
signs a version without gpu-box and posts it. Then:

- The relay closes gpu-box's control socket and every link gpu-box is part
  of, after sending gpu-box the signed list that removes it. gpu-box checks
  it and signs itself out.
- Every other daemon gets the list on its control socket, verifies it, and
  closes every live link whose peer is no longer listed, whichever side
  started it. They refuse gpu-box's key from then on.

Either alone closes the links: the test `TestRevokeDropsLink` runs a relay
that ignores revocations and still sees the link drop in well under 5
seconds, because the daemon on the other end enforces the list itself.
`gecko logout` is a revocation of the machine itself, followed by deleting
its keys.

## Fleet view and the direct path

Signed in, a daemon's reply to `hello` carries its machine id. When an SSH
host turns out to be a machine of the same account, the laptop records the
id in `config.json` (`"machine"` on the host) and shows that machine once,
as the SSH host, with the relay as its fallback: Gecko tries SSH first and
the relay only if SSH fails. It goes back to trying SSH first on the next
reconnect. Every other machine of the account appears under **Cloud**.

Each machine shows whether it is online, when the relay last saw an
offline one, its agents (waiting, working) and whether the live link runs
over the relay.

## Signed out means off

- A machine that has never run `gecko login` has no `cloud/` directory and
  starts nothing: no connection, no files, no extra messages to windows,
  and the sidebar looks as it did before.
- `"cloud": {"enabled": false}` in `config.json` keeps everything off even
  for a signed-in machine (it doesn't connect, and `gecko login` refuses).
  Without that key, being signed in is what turns the feature on. The PRD's
  `cloud.enabled` flag is this setting.
- SSH machines, local sessions and the local token work exactly as before
  when the relay is down or unreachable.

## Identity

Accounts are names on the relay. Anyone can ask to join `roy`, but that only
creates a pending request that a machine already in `roy` has to approve
with the right code. The first `gecko login` for a name creates the account,
so the first person to pick a name on a relay owns it. For a self-hosted
relay that is fine; a public relay needs OIDC.

OIDC fits in one place. The relay would require an ID token on
`POST /v1/enroll` and `POST /v1/account` and bind the account to the token's
subject instead of the typed name. Daemons don't change: they trust the
signed list, not the relay's idea of who is logged in. So a stolen Google
password gets someone a pending request, the same as today.

## Threat model

Mapped to the PRD's threat table.

| Threat | What happens here |
|---|---|
| Stolen OAuth credentials | No OAuth yet. Whatever the relay accepts as a login, a new machine still needs a machine of the account to approve it with the code it shows. Passkey step-up (R14), the audit log (R15) and email on enrollment are not built. |
| Compromised relay | It sees ciphertext, routing metadata (which machine talks to which, when, how much), machine names, OS and addresses. It can't read or alter terminal data (Noise). It can't add a device: daemons verify every list against their own copy, and the Noise handshake fails for a key not on the list (`TestUntrustedRefused`). It can't swap a waiting machine's keys without the code changing. It can withhold updates: refuse service, delay a revocation, or keep a daemon on an old list. It can't roll a daemon back to an older list. |
| Lost or stolen device | Revoke it from any machine. Its links close right away, both because the relay drops them and because every daemon enforces the new list. Its relay requests stop working at once (no tokens to expire). |
| Malicious `gecko login` code phishing | The prompt shows the machine's name, OS and the address it came from, and approving needs the code shown on that machine. Codes expire in 10 minutes. An attacker who gets you to type their code into your machine still gets in, as in the PRD. |
| Push notification leaks prompt text | No push yet. |
| Last trusted device lost | The recovery code adds a machine without approval. Without it, make a new account and sign every machine in again. |
| A trusted machine is compromised | Not in the PRD table. It holds a signing key, so it can approve or revoke anything, including revoking every other machine. It also has shell access to every machine in the account (their own tabs), as with SSH keys today; since protocol 10 it no longer reaches the SSH and link machines behind them through the relay, because peers are served with the local scope only. Revoke it from another machine; whoever publishes first wins, so a fight is possible. |
| A new machine joins a fake account | A compromised relay could show a new machine a different account's chain. The new machine then trusts the attacker's machines. Defence: compare the fingerprint `gecko login` prints with `gecko whoami` on a machine you trust, or pass `--fingerprint`. Your existing machines are not affected. |

Remaining weak spots, to fix before a beta:

- **Pairing code length.** 40 bits. A relay that wants to substitute keys
  must find keys whose hash matches the code within 10 minutes, about 2^40
  key generations. Expensive, not impossible for a well-funded attacker. A
  PAKE (for example CPace) or a longer code removes this.
- **No forward secrecy for the list.** Old signed lists stay valid
  signatures forever; only the version rule stops replays.
- **Freshness.** A relay can delay a revocation for a daemon it serves.
  Daemons don't yet require a recently signed list.
- **The relay holds a control socket per daemon in memory** and pairs links
  in one process. Fine for self-hosting, not for a fleet behind a load
  balancer.
- **No rate limits** beyond 5 pending machines per account.

## Running a relay

```sh
gecko relay --listen 127.0.0.1:7900                                  # this machine only
gecko relay --listen :443 --tls-cert cert.pem --tls-key key.pem       # its own TLS
gecko relay --listen 127.0.0.1:7900 --trust-proxy                     # behind Caddy or nginx
```

Clients refuse a relay over plain http unless it is on the same machine.
Behind a reverse proxy, pass WebSocket upgrades through and set
`--trust-proxy` so the approval prompt shows the real address.

## Open questions for the owner

- Should approval be skippable when `gecko login` runs on a machine that is
  already trusted (PRD open question)? Not built: every new key needs an
  approval.
- Is a 40-bit code enough for the alpha, or do we want a PAKE first?
- Which OIDC providers, and does a self-hosted relay need one at all?
- Should the recovery code be rotatable, and should revoking it be allowed?
