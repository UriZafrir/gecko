# More machines

[Docs](README.md)

```sh
gecko host install devbox      # copies the right binary to devbox (see below)
gecko host add devbox me@devbox
```

You can also add a machine from the sidebar (**Machines → +**). The dialog
checks the SSH connection and shows its output live. If something is wrong,
it explains the error, and you can edit the destination and retry. If Gecko
isn't installed on the machine yet, it offers to copy itself over. A
destination can include a port (`me@host:2222`). Hover a machine and click ×
to remove it; its sessions keep running there.

When you ssh to a machine in a tab, the status line offers **Make it a Gecko
machine**, which opens the same dialog for that destination.

The dialog asks before trusting a machine it hasn't connected to: it shows
the host key's fingerprint, and adds it to `~/.ssh/known_hosts` when you
confirm. If the machine wants a password, the dialog asks for it, and offers
to put your SSH key (`~/.ssh/id_ed25519.pub`, or the phone's key in the
Android app) into `~/.ssh/authorized_keys` there over that same login, like
`ssh-copy-id`. The password goes to ssh through an askpass helper over
Gecko's own socket: it is never on a command line, in the environment, in a
file or in a log, and Gecko forgets it when the check ends. Gecko's lasting
connection to the machine always uses a key, so without one the password
alone isn't enough.

Gecko uses your normal SSH config, keys and agent. That lasting connection
runs ssh with `BatchMode` (no password prompts) and
`StrictHostKeyChecking=accept-new`, and refuses a changed host key. The
remote daemon starts on demand, detached from the SSH session, and keeps
running. On Linux with lingering enabled it runs as a transient systemd user
unit, so logind won't clean it up at logout.

Installing and **Update Gecko** replace the gecko that `gecko bridge` runs
there when the login may write it, and otherwise copy it to
`~/.local/bin/gecko`, which the bridge looks in first (see
[Where gecko lives](install.md#where-gecko-lives)). The copy goes under a
temporary name and is renamed over the old file once it runs there, so the
gecko running there is never overwritten in place. Update Gecko then
restarts the service there with that binary, keeping its tabs, and checks
the build that comes back; a Gecko there from before 0.3 can only restart
into the binary it was started from, and the dialog says what to run there
once.

Any command that runs `gecko bridge` on the other side works as a transport,
for example a container:

```json
{ "name": "box", "command": ["docker", "exec", "-i", "box", "gecko", "bridge"] }
```


## Managing everything from one machine (the anchor)

Machines belong to the Gecko that added them. If you keep one machine on
(a dev box) and open its Gecko from your phone, a tablet or another
computer, that machine is your **anchor**: add every other machine to *it*,
from any of those devices, and they all see the same list. The anchor does
the connecting, with its own SSH keys and from its own network; the phone
never needs to reach the other machines.

Opened from another device, Gecko says so: `via devbox` in the bottom bar,
an **anchor** badge on devbox in the sidebar with its machines listed under
it, and "Add a machine to devbox" in the dialog. Machines added on the
laptop's own Gecko stay the laptop's (add the laptop itself to the anchor
to see its tabs). Adding the same Gecko twice, or a link back to the anchor
itself, is refused.

The anchor is always marked in the sidebar (an anchor icon and a
highlight), and any machine there can become it: long-press or right-click
it and choose **Make anchor**. The page then shows that machine's tabs and
machines (nothing is copied to it), and **Back to** in the sidebar
switches back. Details, the security model and the trade-offs:
[The anchor](anchor.md).

## Another Gecko by its link

A machine already running Gecko can be added by its link instead of over
SSH: the line `gecko url` prints there, or the QR code's link under **Open
on another device**. This Gecko then connects straight to that one's web
address, over the same WebSocket and token the web UI uses, and shows its
tabs and agents next to its own. It is how the Android app makes Gecko in
Termux one of This phone's machines, and it works for any Gecko on this
device, the LAN or a tailnet.

```sh
gecko host add laptop --url 'http://100.101.102.103:7681/?token=...'
gecko url | gecko host add termux --url -     # the link on stdin, kept out of ps
```

In the sidebar it is **Machines → + → Paste a Gecko link**: the dialog
connects once, shows the other Gecko's name and system, and adds it. A link
machine reconnects and resumes its tabs like an SSH one, shows its round
trip, and its agents notify here. `--no-check` adds it without connecting
first (the Android app does, for a Termux that hasn't started yet).

How it's kept safe:

- The token is the other Gecko's whole key, so it is treated as a secret:
  it is stored only in this Gecko's `config.json` (mode `0600`), sent only
  in the `Authorization` header, and never shown: not in the sidebar, the
  machine list (`gecko host ls` shows just the address), errors or logs.
  The dialog and the CLI send it as a secret field, not as a target.
- Plain `http` is allowed only to this device, private networks (10/8,
  172.16/12, 192.168/16, IPv6 ULA, link-local) and Tailscale (100.64/10),
  the same rule as the mobile apps. A name must resolve only to such
  addresses, and the connection itself is refused if it would go anywhere
  else (so a name that changes later can't send the token out). Anything
  else needs `https`.
- Only the Gecko you use directly can add one: a Gecko connected through
  another (a bridge, a link or the relay) can't make it dial addresses on
  its behalf, remove its machines, or reach their tabs. The server enforces
  this since protocol 10.
- The other Gecko sees an ordinary client with its token. Its machines
  aren't passed on (as with SSH machines, each Gecko shows only its own
  tabs to another), and updating it is done there (`gecko upgrade`).

A wrong or changed token shows "refused the token" on the machine; paste
the current link (**Fix connection…**) to replace it.

## Through the relay (accounts)

For machines SSH can't reach (behind NAT or a corporate firewall), or when no
single machine should be the hub, sign each one in to an account on a relay.
Every daemon dials out to the relay over HTTPS (port 443, `HTTPS_PROXY`
honored); nothing listens for inbound connections. See [the spec](cloud.md)
for how it works and what the relay can and can't see.

Run a relay somewhere both machines can reach (it is the same binary):

```sh
gecko relay --listen :443 --tls-cert cert.pem --tls-key key.pem
```

On the first machine:

```sh
gecko login --relay https://relay.example.com --account roy
```

This creates the account and prints a **recovery code**, once. Keep it: it
adds a machine when none of yours is around.

On every other machine, the same command prints a code and waits:

```
    code: ANHI-H4TC   (expires in 10 minutes)
```

Every signed-in machine shows "gpu-box (Linux, 203.0.113.4) wants to join.
Approve?". Click **Approve…** and type the code, or run
`gecko approve ANHI-H4TC` there. The machine appears under **Machines →
Cloud** on all of them, and you open tabs on it like on any other machine.

- `gecko whoami` shows the account, its fingerprint, every machine (online,
  last seen, address) and who is waiting.
- `gecko revoke gpu-box`, or **Revoke** in **Devices** (the screen icon next to
  Machines), removes a machine; its connections close within seconds.
- `gecko logout` takes this machine out and deletes its keys.
- A machine that is also an SSH host shows once. Gecko uses SSH and falls
  back to the relay when SSH can't connect.
- `"cloud": {"enabled": false}` in `config.json` turns all of it off on that
  machine, even when signed in.
