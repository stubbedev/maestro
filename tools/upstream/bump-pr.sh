#!/usr/bin/env bash
# Opens the PR that bumps the pin to the newest stable Composer release
# newer than it (tools/upstream/bump.sh on branch upstream/composer-<tag>),
# once: an existing branch means it is open or was dealt with. The PR and
# the release's issue (tools/upstream/open-issues.sh) link each other.
# .github/workflows/upstream-composer.yml runs it after open-issues.sh.
#
# Needs git (a checkout of main that may push), gh (GH_TOKEN), jq, php,
# curl, perl and go.
#
# Usage: tools/upstream/bump-pr.sh [--dry-run]
# --dry-run says which release it would bump to, and changes nothing.
set -euo pipefail
# shellcheck source=tools/upstream/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$upstream_root"
dry=${1:-}

tag=
while IFS=$'\t' read -r t pre _; do
	if [ "$pre" != true ] && { [ -z "$tag" ] || newer "$t" "$tag"; }; then
		tag=$t
	fi
done < <(releases)
if [ -z "$tag" ]; then
	echo "Composer $pinned is the newest stable release"
	exit 0
fi

branch=upstream/composer-$tag
if git ls-remote --exit-code --heads origin "$branch" >/dev/null; then
	echo "Composer $tag: $branch exists"
	exit 0
fi

if [ "$dry" = --dry-run ]; then
	echo "would bump Composer $pinned -> $tag on $branch (issue: $(release_issue "$tag"))"
	exit 0
fi

tools/upstream/bump.sh "$tag"

issue=$(release_issue "$tag")
refs=
[ -n "$issue" ] && refs="Refs #$issue"

git switch -c "$branch"
git -c user.name='github-actions[bot]' -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
	commit -qam "upstream: pin composer $tag" -m "tools/upstream/bump.sh $tag: the version, release date, API versions and phar checksum in internal/upstream, the shim's constants and the docs. Porting the release's changes follows on this branch.${refs:+

$refs}"
git push -q origin "$branch"

# Without "Allow GitHub Actions to create and approve pull requests" in
# the repository's settings the token cannot open the PR: the branch is
# pushed either way, and the issue links whichever exists.
if pr=$(gh pr create --base main --head "$branch" --title "upstream: pin Composer $tag" --body "\`tools/upstream/bump.sh $tag\` moved the pin (internal/upstream, the shim's constants, the docs). Port the release's changes on this branch before merging (docs/PORTING.md, \"Following upstream\").${refs:+

$refs}"); then
	link="Bump PR: $pr"
else
	link="Bump branch (opening its PR failed; see the workflow run): $(gh repo view --json url -q .url)/compare/main...$branch"
	status=1
fi
echo "$link"
if [ -n "$issue" ]; then
	gh issue comment "$issue" --body "$link"
fi
exit "${status:-0}"
