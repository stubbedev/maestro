#!/usr/bin/env bash
# Opens one issue for every Composer release newer than the one maestro
# ports (docs/PORTING.md, "Following upstream"), pre-releases included and
# titled as such. Each issue carries a hidden marker,
# <!-- composer-release: X -->; a release an `upstream` issue (open or
# closed) already marks is skipped, so it runs as often as it likes.
# .github/workflows/upstream-composer.yml runs it daily.
#
# Needs gh (GH_TOKEN), jq and php (version_compare orders releases as
# Composer does, RCs before their release).
#
# Usage (from the repo root): tools/upstream/open-issues.sh [--dry-run]
# --dry-run prints the issues instead of opening them.
set -euo pipefail

repo=composer/composer
label=upstream
dry=${1:-}
pinned=$(tools/upstream/composer-version.sh)
export LC_ALL=C

newer() { php -r 'exit(version_compare($argv[1], $argv[2], ">") ? 0 : 1);' "$1" "$2"; }

# composer.lock's "name version" lines at a ref
lock() {
	gh api "repos/$repo/contents/composer.lock?ref=$1" -H 'Accept: application/vnd.github.raw+json' |
		jq -r '.packages[] | "\(.name) \(.version)"' | sort
}

# the changed files under the paths that matter, grouped
diffstat() {
	gh api "repos/$repo/compare/$pinned...$1" --jq '.files[] | [.filename, .additions, .deletions] | @tsv' |
		awk -F'\t' '
			BEGIN {
				n = split("Repository/ and Downloader/: wire protocol, metadata, transport|" \
					"Package/, Installer/ and res/*.json: lock, installed.json and schema formats|" \
					"Command/ and Console/: CLI surface, options, output|" \
					"Plugin/ and EventDispatcher/: plugin API (and tools/shimgen'"'"'s golden)|" \
					"composer.lock: bundled library versions", names, "|")
			}
			$1 ~ /^src\/Composer\/(Repository|Downloader)\//          { g = 1 }
			$1 ~ /^src\/Composer\/(Package|Installer)\/|^res\/.*\.json$/ { g = 2 }
			$1 ~ /^src\/Composer\/(Command|Console)\//                 { g = 3 }
			$1 ~ /^src\/Composer\/(Plugin|EventDispatcher)\//          { g = 4 }
			$1 == "composer.lock"                                       { g = 5 }
			g { files[g]++; add[g] += $2; del[g] += $3; list[g] = list[g] sprintf("- `%s` +%d -%d\n", $1, $2, $3); g = 0 }
			END {
				for (i = 1; i <= n; i++) {
					if (!files[i]) { printf "**%s**: no changes\n\n", names[i]; continue }
					printf "<details><summary><b>%s</b>: %d files, +%d -%d</summary>\n\n%s\n</details>\n\n", names[i], files[i], add[i], del[i], list[i]
				}
			}'
}

if [ "$dry" != --dry-run ]; then
	gh label create "$label" --color 0e8a16 --description "A new Composer release to port" --force >/dev/null
fi
existing=$(gh issue list --label "$label" --state all --limit 1000 --json body --jq '.[].body' || true)

gh api "repos/$repo/releases?per_page=50" --jq '.[] | select(.draft | not) | [.tag_name, .prerelease, .html_url] | @tsv' |
	while IFS=$'\t' read -r tag pre url; do
		newer "$tag" "$pinned" || continue
		marker="<!-- composer-release: $tag -->"
		if grep -qF -- "$marker" <<<"$existing"; then
			echo "Composer $tag: already has an issue"
			continue
		fi

		title="Upstream: Composer $tag"
		[ "$pre" = true ] && title+=" (pre-release)"
		body=$(mktemp)
		{
			echo "$marker"
			echo "Composer [$tag]($url) is out; maestro ports $pinned. Triage what changed and port it (docs/PORTING.md, \"Following upstream\")."
			echo
			echo "Compare: https://github.com/$repo/compare/$pinned...$tag"
			echo
			echo "## Release notes"
			echo
			gh api "repos/$repo/releases/tags/$tag" --jq .body
			echo
			echo "## Changes that matter most"
			echo
			diffstat "$tag"
			echo "## Bundled libraries (composer.lock)"
			echo
			join -a1 -a2 -e - -o 0,1.2,2.2 <(lock "$pinned") <(lock "$tag") |
				awk '$2 != $3 { printf "- `%s`: %s -> %s\n", $1, $2, $3; c++ } END { if (!c) print "No changes." }'
			echo
			echo "## Checklist"
			echo
			echo "- [ ] Bump the pin: \`ComposerVersion\` in internal/util/http/runtime.go (read by everything else), \`ReleaseDate\` in internal/composer, the e2e phar's \`composerPharSHA256\`, the shim's \`Composer::VERSION\` and \`RELEASE_DATE\`"
			echo "- [ ] Port the changes (ref-sync, then diff .ref/composer)"
			echo "- [ ] Regenerate the oracles (\`tools/oracle/*\`, the errors oracle) and the shim stubs (\`tools/shimgen\`, \`tools/shimvendor\`)"
			echo "- [ ] Port the new or changed PHPUnit tests"
			echo "- [ ] \`just e2e\` green against the new phar"
			echo "- [ ] Update the version references in docs/PORTING.md and docs/PLUGINS.md"
		} >"$body"

		if [ "$dry" = --dry-run ]; then
			echo "=== $title"
			cat "$body"
		else
			gh issue create --title "$title" --label "$label" --body-file "$body"
		fi
		rm -f "$body"
	done
