#!/bin/sh
# Attaches files to the draft GitHub release for a tag, creating the draft
# when there is none. The Linux binaries, Desktop and Linux packages
# workflows all run this for the same v* tag, so they share one draft; if two
# create it at the same moment, the oldest is kept and the others (still
# empty) are deleted. Nothing is published: a person reviews the draft.
#   R=owner/repo TAG=v1.2.3 GH_TOKEN=... .github/scripts/release-attach.sh file...
set -eu
: "${R:?}" "${TAG:?}"
[ $# -gt 0 ] || { echo "release-attach: no files to attach" >&2; exit 1; }

releases() { # ids of the releases for $TAG, oldest first
	gh api "repos/$R/releases?per_page=100" --jq "[.[] | select(.tag_name == \"$TAG\")] | sort_by(.id) | .[].id"
}

if [ -z "$(releases)" ]; then
	gh release create "$TAG" -R "$R" --draft --verify-tag --title "Gecko ${TAG#v}" \
		--notes "Built by the Linux binaries, Desktop and Linux packages workflows. Draft: review before publishing." ||
		echo "release-attach: create failed, another workflow may have made the draft" >&2
	sleep 10
	releases | tail -n +2 | while read -r id; do
		if [ "$(gh api "repos/$R/releases/$id" --jq '.draft and (.assets | length == 0)')" = true ]; then
			echo "release-attach: deleting duplicate draft $id"
			gh api -X DELETE "repos/$R/releases/$id" || true
		fi
	done
fi
n=$(releases | wc -l)
[ "$n" -eq 1 ] || { echo "release-attach: $n releases for $TAG, expected one" >&2; exit 1; }
for f in "$@"; do echo "attaching $f ($(wc -c <"$f") bytes)"; done
gh release upload "$TAG" -R "$R" --clobber "$@"
