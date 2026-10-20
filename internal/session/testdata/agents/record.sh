#!/bin/sh
# Records the agent screens in this directory again: each agent runs in a
# pty at a fixed size against the stand-in model (internal/agentmock), from
# a fresh config of its own, driven by the .rec scripts next to its
# fixtures. Usage, from the repository root:
#
#	internal/session/testdata/agents/record.sh codex|opencode|opencode-real [workdir]
#
# workdir (default ~/gecko-agent-demo) is where the agent runs; it gets a
# git repository with one file, a.txt.
set -eu
agent=$1
work=${2:-$HOME/gecko-agent-demo}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../../.." && pwd)
tmp=$(mktemp -d)
# Codex leaves a background app-server of its own running from its home.
trap 'pkill -f "$tmp/" || true; rm -rf "$tmp"' EXIT
mkdir -p "$work"
[ -d "$work/.git" ] || git -C "$work" init -q -b main
[ -f "$work/a.txt" ] || echo hello >"$work/a.txt"
(cd "$root" && go build -o "$tmp/rec" ./internal/session/testdata/agents/rec)
out=$here/$agent
run() { # size script args...
	size=$1 script=$2
	shift 2
	(cd "$work" && "$tmp/rec" -cols "${size%x*}" -rows "${size#*x}" -mock "$tmp/log" -out "$out" -agent "$version" -script "$out/$script" -timeout 3m -- "$@" </dev/null)
}
mkdir -p "$tmp/log"
case $agent in
codex)
	version=$(codex --version </dev/null)
	fresh() {
		rm -rf "$tmp/home"
		mkdir -p "$tmp/home"
		printf 'model = "mock-model"\nmodel_provider = "mock"\napproval_policy = "on-request"\nsandbox_mode = "read-only"\n' >"$tmp/home/config.toml"
	}
	mock='model_providers.mock={name="Mock",base_url="{{MOCK}}",wire_api="responses"}'
	fresh
	run 100x30 record.rec env CODEX_HOME="$tmp/home" codex -c "$mock"
	for size in 50x30 160x40; do
		fresh
		run $size record-size.rec env CODEX_HOME="$tmp/home" codex -c "$mock"
	done
	# No provider and no account: the sign-in screen.
	rm -rf "$tmp/home" && mkdir -p "$tmp/home"
	run 100x30 record-login.rec env CODEX_HOME="$tmp/home" codex
	;;
opencode)
	version="opencode $(opencode --version </dev/null)"
	config='{"provider":{"mock":{"npm":"@ai-sdk/openai-compatible","name":"Mock","options":{"baseURL":"{{MOCK}}"},"models":{"mock-model":{"name":"Mock Model"}}}},"model":"mock/mock-model","small_model":"mock/mock-model","permission":{"bash":"ask"},"autoupdate":false,"share":"disabled"}'
	cache=${XDG_CACHE_HOME:-$HOME/.cache}/gecko-test/opencode
	fresh() {
		rm -rf "$tmp/data" "$tmp/state" "$tmp/config"
		mkdir -p "$tmp/data" "$tmp/state" "$tmp/config" "$cache"
	}
	fresh
	run 100x30 record.rec env OPENCODE_CONFIG_CONTENT="$config" XDG_DATA_HOME="$tmp/data" XDG_STATE_HOME="$tmp/state" XDG_CONFIG_HOME="$tmp/config" XDG_CACHE_HOME="$cache" opencode
	for size in 50x30 160x40; do
		fresh
		run $size record-size.rec env OPENCODE_CONFIG_CONTENT="$config" XDG_DATA_HOME="$tmp/data" XDG_STATE_HOME="$tmp/state" XDG_CONFIG_HOME="$tmp/config" XDG_CACHE_HOME="$cache" opencode
	done
	;;
opencode-real)
	# OpenCode as it is set up here (its model and account), in data and
	# state directories of its own, with bash set to ask. Three tiny
	# prompts, so it costs a little. OPENCODE_MODEL picks the model.
	version="opencode $(opencode --version </dev/null)"
	out=$here/opencode
	config='{"permission":{"bash":"ask"},"autoupdate":false,"share":"disabled"'${OPENCODE_MODEL:+',"model":"'$OPENCODE_MODEL'"'}'}'
	mkdir -p "$tmp/data" "$tmp/state"
	(cd "$work" && "$tmp/rec" -cols 100 -rows 30 -out "$out" -agent "$version, ${OPENCODE_MODEL:-its default model}" -script "$out/record-real.rec" -timeout 6m -- \
		env OPENCODE_CONFIG_CONTENT="$config" XDG_DATA_HOME="$tmp/data" XDG_STATE_HOME="$tmp/state" opencode </dev/null)
	;;
*)
	echo "unknown agent $agent" >&2
	exit 2
	;;
esac
