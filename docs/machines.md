# More machines

[Docs](README.md)

```sh
gecko host install devbox      # copies the right binary to devbox:~/.local/bin
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

Any command that runs `gecko bridge` on the other side works as a transport,
for example a container:

```json
{ "name": "box", "command": ["docker", "exec", "-i", "box", "gecko", "bridge"] }
```

