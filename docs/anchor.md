# The anchor: one Gecko that manages the rest

[Docs](README.md)

Status: built in protocol 10; any machine can be made the anchor since
protocol 11 (see [Make anchor](#make-anchor-protocol-11)).

You keep one machine on all the time, say your dev box, and open its Gecko
from everywhere else: the phone (the web app through `gecko share`,
Tailscale Serve or the Android/iOS app), a tablet, another computer's
browser. That machine is the **anchor**. It connects out, over SSH, a Gecko
link or the relay, to every other machine you care about, and anything you
do from the phone is done by the anchor: tabs open on the machines it
reaches, machines are added and removed in its list, agents notify from it.

The anchor is not a new kind of daemon. Every Gecko already federates the
machines in its own list (its *hub*). The anchor is simply the Gecko you
are connected to when you aren't sitting at it, and this page is about
making that clear and safe.

## What the owner hit (reproduced)

Five throwaway daemons on one box (ports 7910 to 7915, separate
`GECKO_HOME`s): `devbox` with a machine `gpu` (added by its link),
`laptop` with a machine `buildbox`, a `phone` daemon (Android's This
phone), and the phone's web app connected to devbox through
`gecko share` (and, separately, through the main web port as Tailscale
Serve would).

| Phone connected to devbox | Machines in the sidebar | Tabs |
|---|---|---|
| No account | devbox, gpu | devbox, gpu |
| Everything signed in to one account | devbox, gpu, laptop, phone | devbox, gpu, laptop, phone |
| After adding laptop **from the phone** (Paste a Gecko link) | devbox, gpu, laptop | devbox, gpu, laptop |

So the mechanics work: the phone sees and drives everything devbox's own
hub has, including machines added from the phone. What goes wrong:

1. **Machines live in the hub that added them.** Machines added on the
   laptop (`buildbox`) are in the laptop's list, not devbox's, and tabs
   started on the laptop's own Gecko only show on devbox once devbox has the
   laptop as a machine. Federation is one level deep by design (it can't
   loop), so nothing "flows up" to the anchor.
2. **Nothing tells you which machine you are on.** The phone's sidebar says
   `devbox · this machine`, which reads as the phone. The status line, which
   would name the machine, is hidden on phones. The Add machine dialog says
   "Gecko connects with your SSH keys", but it's devbox's keys, and "plain
   http works only on this device" means devbox.
3. **Reaching a laptop from the anchor isn't obvious.** A laptop's Gecko
   listens on 127.0.0.1, so its `gecko url` is useless to devbox. `gecko
   share on` gives a link, but its token is new every time and dies when
   the laptop restarts or upgrades, so a link machine built on it breaks.
   The ways that last are SSH (the laptop runs sshd) and the account (both
   dial out to the relay, so NAT doesn't matter).
4. **Security finding #1 stands in the way.** Every Gecko that connects to
   another one picks its own scope. A peer through the relay that leaves
   `scope` out gets the anchor's whole hub: its SSH machines, link machines
   (whose tokens it holds), approve and revoke. An anchor concentrates
   reach, so this has to be enforced by the server before we encourage it.

## The model

- **The anchor is the Gecko you connect to.** Its hub is the source of
  truth: its list of machines (`config.json`), its SSH keys and known
  hosts, its link tokens, its account.
- **A client of the anchor manages that list.** From the phone you add a
  machine (SSH or a Gecko link), check it, install Gecko there, update it,
  open tabs on it and remove it, exactly as at the anchor's own keyboard.
  The anchor dials; the phone never needs to reach the other machines.
- **Clients don't bring machines.** A phone or laptop opening the anchor's
  UI is a client, not a peer. Its own machines stay in its own list.

### Do machines added on other devices sync to the anchor?

No, deliberately. A machine entry is a credential: an SSH destination that
works with *that* device's keys, or a link with another Gecko's token.
Copying them to the anchor would quietly widen what a compromise of the
anchor gives away, and SSH entries wouldn't work there anyway without the
same keys. Instead:

- add the machine to the anchor once, from any client of it (the phone
  included), and it's there for every client from then on;
- machines in your account are deduplicated by machine id, so a laptop that
  is both an SSH host and an account machine shows once (unchanged);
- a link and an SSH entry (or two links) that reach the *same running
  Gecko* are now detected: the second one shows "already connected as
  laptop" instead of listing every tab twice, and a link that leads back to
  the anchor itself is refused ("that link leads to devbox itself").

### Where new tabs run

Where they always did: on the machine you pick, defaulting to the machine
of the current tab, then the anchor. Nothing runs on the phone unless the
phone is itself a machine of the anchor.

### Agents and notifications

Agents are detected on the machine they run on and reported up the link,
so the anchor knows every agent on every machine it reaches. The phone gets
them over its connection to the anchor (the dock, the sidebar counts, the
Android/iOS watchers), and the anchor's webhook (`notify.webhook`) fires for
all of them. One anchor, one place notifications come from.

### No loops, no duplicates

- Every hub-to-hub link is served with the **local** scope, now enforced by
  the server for relay peers and honoured for everything else: a machine
  only ever shows its own tabs to the Gecko that dialed it. A cycle (devbox
  has laptop, laptop has devbox) stays two one-level views.
- Each running Gecko has a random **node id** for its lifetime, sent in the
  hello reply (protocol 10). A hub refuses a link whose node id is its own
  (a loop through a link or SSH back to itself) and marks a second link to a
  node it already reaches as a duplicate, so the same tab never shows twice.

## Security model

Trust comes from **how a connection was made**, never from what the other
side says in `hello`. A client can always ask for less; it can't get more.

| Connection | Who can make it | Granted |
|---|---|---|
| Unix socket (`gecko` CLI, the Android app, `gecko bridge` over SSH) | this user on this machine, or someone with SSH as this user | owner (full) |
| Web UI with the main token (browser, desktop app, PWA through Tailscale Serve, a link hub holding that token) | whoever has the token | owner (full) |
| Web UI with the share token (`gecko share`, "Allow other devices") | whoever has that link, from a private or tailnet address | owner (full) |
| Relay peer (another machine of the account, through the relay) | an approved machine on the signed device list | **peer: this machine's own tabs only** |

A **peer** is limited by the server whatever it asks for:

- it sees and drives only the anchor's own sessions (list, create, attach,
  type, resize, kill, capture, search, folders, tmux on this machine);
- ids of other machines' sessions don't resolve for it (`unknown host`),
  so it can't reach a tab on the anchor's SSH or link machines;
- it can't add, remove, check, install, trust or update machines, open
  workspace templates, change settings, restart the service, turn sharing
  on, or touch the account (approve, deny, revoke, logout).

This fixes finding #1 for the relay: one compromised account machine
reaches the other machines' own tabs (it is an approved machine of yours),
but no longer every SSH host and link behind them, and can't approve new
devices through a peer. The same restrictions also apply to any client that
*asks* for the local scope (link hubs and SSH bridges do), which closes the
gap where a "local" client could still name another machine's tab by id.

**Owner clients are not narrowed.** The phone holding the anchor's token is
you; it can do everything the anchor's own window can, including adding
machines with the anchor's SSH keys. That is the point of an anchor.

Trade-off, stated plainly: the share link (`gecko share`) is also an owner
credential. It is new every time, works only from private and tailnet
addresses and dies with `share off`, but while it lives it can add machines
and approve account devices like the main token. We kept it that way
because the share listener is exactly how most phones reach their anchor;
narrowing it would make the anchor unmanageable from the phone. If you want
a read-and-type-only phone, that would be a third grant (see
[Open questions](#open-questions)).

Revocation:

- remove a machine on the anchor (from any client): its link closes, its
  tabs leave every client at once, and they keep running on that machine;
- `gecko share off`: every client of the share link is disconnected, and
  the link is dead;
- revoke an account machine: the relay and every peer close its links
  (unchanged);
- rotate the main token: delete `~/.gecko-terminal/token` and restart.

## Failure modes

- **The anchor goes down.** Every tab on every *other* machine keeps
  running; those daemons own them. When the anchor comes back (or after
  `gecko upgrade`) it redials every machine and every client resumes its
  tabs by byte offset. While it's down the phone shows "reconnecting"; you
  can still open another machine's Gecko directly if you have its link.
- **A machine is offline.** It stays in the anchor's list with its last
  error (or "seen 5m ago" for an account machine), its tabs disappear from
  the sidebar until it's back, and the anchor redials with backoff (1 s to
  30 s, at once when the relay says it's online).
- **The phone's network drops.** The phone reconnects to the anchor and
  resumes; nothing changes on the machines.
- **The anchor is upgraded.** `gecko upgrade` keeps its sessions and
  listeners; links redial (the share listener comes back off, as before).

## Protocol 10

Additive, as every version before:

- **hello reply** gains `scope`, the scope the server actually granted
  (`all` or `local`), and `node`, the random id of this running Gecko.
- **Server-enforced scope**: relay peers always get `local`; a `local`
  client can't resolve another machine's session id, and the requests
  listed above are refused with an error.

Compatibility:

- A v10 anchor with v9 machines: works. Older machines send no `node`, so
  duplicate and loop detection only covers v10 Geckos.
- A v9 Gecko dialing a v10 one over the relay: works for its own purpose
  (it always asked for `local`). A v9 client that relied on a relay peer
  getting `all` (nothing in Gecko did) now gets `local`.
- The web UI ignores the new fields on older daemons and shows the anchor
  labels from what it already knows (the hello's host name and the page's
  address), so a v10 page on a v9 anchor still labels it.

## What the UI shows

The sidebar always marks the anchor, wherever the page runs: an anchor
icon and the word **anchor** next to its name, a highlighted row with an
accent edge, readable in light and dark themes and at 360 dp. Under the
name it says where the anchor is: "this machine" (its own window, the
desktop app, Android's This phone), "connected here" (the Gecko you opened
from another device) or "through laptop" (an anchor you picked, reached
through laptop).

Whenever the anchor isn't the device in your hand:

- **Bottom bar**: `via devbox` with an anchor mark, on desktop and on
  phones (where the status line is otherwise hidden it shows as a slim bar
  under the terminal).
- **Sidebar**: the anchor first, every machine it reaches listed under
  it, indented, with how it's reached and its round trip.
- **Add a machine**: titled "Add a machine to devbox", and the text says
  devbox's SSH keys and devbox's network, so it's clear who dials.

## Make anchor (protocol 11)

Any machine in the sidebar can become the anchor. Long-press it (or
right-click it), choose **Make anchor**, and confirm; the command palette
has "Make ubuntu the anchor" too. The page reconnects and from then on
shows ubuntu's tabs and machines, ubuntu dials the rest, the icon moves to
ubuntu and the bottom bar says `via ubuntu`.

How it works: the page still connects to the same Gecko (the **door**: the
desktop app's own daemon, This phone, the Gecko a browser tab opened). It
says hello with the path to the anchor (`"anchor": ["ubuntu"]`), and the
door forwards the whole connection to ubuntu over the link it already has
to ubuntu (SSH bridge, Gecko link or relay), asking ubuntu for the full
scope. A path can go through up to 4 Geckos (ubuntu, then one of
ubuntu's machines, and so on); each hop forwards to the next.

Nothing is copied. ubuntu shows only its own machines: no link tokens, SSH
entries or keys move from the door to ubuntu. So you're never stranded,
the sidebar keeps a **Back to** list of the anchors you came through (the
door at least); choosing one, or Make anchor on the door or This phone,
switches back.

The choice is remembered per device:

| Device | Where the anchor is kept |
|---|---|
| Desktop app (macOS, Windows, Linux) | the app's settings (`anchor.json` in its user data) |
| Browser tab, PWA (iOS Safari, Android Chrome) | the page's storage, per Gecko address |
| Android app (This phone and machines opened directly) | the app's preferences, per saved machine |
| iOS app (saved machines) | the page's storage, per saved machine's address |
| Termux | a machine like any other: it can be the anchor, or the door |

If a remembered anchor can't be reached on launch, the page stays on the
door and says so, with **Try again** and **Stay on devbox**.

### Who may switch

Only owner clients (the web token, the share link, the socket) may send
a path; the door refuses it from a relay peer or from a client that asked
for the local scope. The door dials the anchor with *its own* credentials
for that machine, so switching never gives the page more than the door's
owner already had through that link:

- **SSH and Gecko links** already reach the other Gecko as its owner (an
  SSH login as you, or its main token): the anchor serves the page in full.
- **The relay** is different: another machine of your account is a peer
  there, and a peer must never become an owner by asking. A machine served
  through the relay only accepts a forwarded page when its owner turned on
  **Can be the anchor through my account** (the Devices dialog, or
  `gecko anchor account on` on that machine). It is off by default. With
  it off, Make anchor says how to turn it on.

### Compatibility

- Protocol 11 is additive: the hello carries `anchor` and `door`, the
  reply carries `door`, and a failed switch comes back as an `anchor`
  error. Clients that don't send a path are unchanged.
- An older target: "Update Gecko on ubuntu to make it the anchor".
- An older door: the page keeps the door as the anchor and says
  "Update Gecko on devbox to choose another anchor from here".

## What changed in the code

- `internal/server`: `ServePeer` for relay peers; a `grant` per client;
  `local` scope enforced in `Resolve` (via `hub.ResolveScope`) and on every
  hub-changing request; hello reply carries `scope` and `node`.
- `internal/daemon`: relay peers are served with `ServePeer`.
- `internal/hub`: node id per running hub; loop and duplicate detection on
  connect; `ProbeLink` refuses a link to the hub itself.
- `internal/proto`: version 10, `Msg.Node`.
- `web/src/anchor.js` (+ tests): when the page is remote, the bottom-bar
  label, the anchor badge and grouping in the sidebar, and the Add machine
  copy.

Protocol 11 (Make anchor):

- `internal/proto`: version 11, `Msg.Anchor`, `Msg.Door`, `TAnchor`.
- `internal/hub/anchor.go`: `OpenAnchor` dials the next Gecko on the path
  over the machine's existing transport and checks its reply.
- `internal/server/anchor.go`: who may forward, the 4 hop limit, the relay
  opt-in (`anchor.fromAccount`), and the forwarding itself.
- `cmd/gecko`: `gecko anchor account [on|off]`.
- `web/src/anchor.js`, `main.js`: the icon and highlight in every mode,
  Make anchor, Back to, the remembered path; the desktop app, Android and
  iOS keep it natively.

## Not built (follow-ups)

- **A lasting link to a laptop without SSH or an account.** A laptop
  behind NAT that doesn't run sshd can only be reached through the account
  today, or through a share link that dies with every restart. A
  persistent "let my anchor in" link (its own token, kept across restarts,
  revocable) would close that gap.
- **This phone as a machine of the anchor.** The Android app's Gecko
  listens on 127.0.0.1 and can't sign in to an account from the app, so the
  anchor can't reach the phone's own tabs. Signing This phone in to the
  account from the app would make it an account machine the anchor reaches
  through the relay.
- **Showing a device's own machines nested under it** (laptop › buildbox)
  through the anchor, opt-in per device. It needs nested session ids and a
  per-device grant; we left it out until someone needs it, since adding the
  machine to the anchor directly works today.

## Open questions

- Should the share link get a narrower grant (tabs only, no machine or
  account management) with an explicit "allow managing machines" switch?
  Safer for a shared screen, but it would break managing the anchor from a
  phone that uses `gecko share`, which is the common setup.
- Should an account have a designated anchor (shown on every device, so the
  laptop's own window can say "devbox is your anchor")?
