# shellcheck shell=bash
# What the tools/upstream scripts share; sourced, not run. Needs gh
# (GH_TOKEN) and php (version_compare orders releases as Composer does,
# RCs before their release).

upstream_repo=composer/composer
upstream_label=upstream
upstream_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
pinned=$("$upstream_root/tools/upstream/composer-version.sh")

# newer A B: whether release A is newer than release B
newer() { php -r 'exit(version_compare($argv[1], $argv[2], ">") ? 0 : 1);' "$1" "$2"; }

# releases: "tag<TAB>prerelease<TAB>url" of Composer's releases newer than
# the pin, drafts left out, newest first
releases() {
	gh api "repos/$upstream_repo/releases?per_page=50" \
		--jq '.[] | select(.draft | not) | [.tag_name, .prerelease, .html_url] | @tsv' |
		while IFS=$'\t' read -r tag pre url; do
			if newer "$tag" "$pinned"; then
				printf '%s\t%s\t%s\n' "$tag" "$pre" "$url"
			fi
		done
}

# marker TAG: the hidden line that identifies a release's issue
marker() { echo "<!-- composer-release: $1 -->"; }

# release_issue TAG: the number of the release's issue, open or closed
release_issue() {
	gh issue list --label "$upstream_label" --state all --limit 1000 --json number,body |
		jq -r --arg m "$(marker "$1")" '[.[] | select(.body | contains($m)) | .number][0] // empty'
}
