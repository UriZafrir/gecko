# Agent screens

Screens of real coding agents, as Gecko's own vt sees them, for the tests in
`agentfixture_test.go`: each agent ran in a pty at a fixed size and was
driven through its states by a script, and each file is the screen at one
of them.

| Agent    | Version                        | Sizes                 |
|----------|--------------------------------|-----------------------|
| codex    | codex-cli 0.163.0 (npm)        | 100x30, 50x30, 160x40 |
| opencode | opencode 1.19.3 (snap)         | 100x30, 50x30, 160x40 |

A file starts with a header, then `#---`, then the screen:

    # agent: codex-cli 0.163.0
    # state: approval
    # size: 100x30, recorded 2026-10-20 with testdata/agents/rec
    # title: [ . ] Action Required | gecko-agent-demo
    # osc7501: state=blocked:app=codex
    #---

`title` is the terminal title the agent had set (OSC 0/2) and `osc7501`
the state it last reported (Codex sends `state=idle|working|blocked` with
OSC 7501). The tests replay both, so what they check is what the daemon
reports.

## The model

The agents talk to `internal/agentmock`, a stand-in for the model on
127.0.0.1, so no account or key is needed and the screens are the same
each time. What it answers depends on the prompt: "slow" streams a long
answer, "list files" calls the shell tool to run `ls` (Codex asks to run
it outside its sandbox, OpenCode has bash set to ask), "ask me" calls the
agent's question tool, anything else gets a one line greeting.

The `real-*.txt` screens of OpenCode are from a real model (Kimi K3 on
OpenRouter), to check the stand-in doesn't hide anything a real one shows
(its folded "Thought" steps, say). `codex/login.txt` is Codex with no
account and no model set up.

## Recording again

From the repository root, with the agent on PATH:

    internal/session/testdata/agents/record.sh codex
    internal/session/testdata/agents/record.sh opencode
    OPENCODE_MODEL=openrouter/moonshotai/kimi-k3 internal/session/testdata/agents/record.sh opencode-real

Each agent gets a fresh home of its own (CODEX_HOME, OpenCode's XDG
directories), so nothing of yours is read or changed. `opencode-real`
needs the provider's key where OpenCode looks for it in the environment
(OPENROUTER_API_KEY, say) and sends three tiny prompts, about a cent
each. The scripts are the `.rec` files next to the screens (see
`rec/main.go` for what a script can say). After a new version, record
again, update the expectations in `agentfixture_test.go` and the table
above.
