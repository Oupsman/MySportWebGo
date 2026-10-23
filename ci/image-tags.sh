#!/bin/sh
# Writes .tags, read by the docker-buildx plugin of the Woodpecker pipeline:
# - a version tag vX.Y.Z (or X.Y.Z): X.Y.Z, X.Y and X, to deploy a fixed version or follow a major one
# - any other tag (1.2.0-rc1...): itself only
# - otherwise (the default branch): latest and the short commit sha, to roll back to a known image
set -eu

# a Docker tag: letters, digits, _ . - and 128 characters at most
sanitize() {
	printf '%s' "$1" | tr -c 'A-Za-z0-9_.-' '-' | cut -c1-128
}

if [ "${CI_PIPELINE_EVENT:-}" = "tag" ]; then
	version=$(sanitize "${CI_COMMIT_TAG#v}")
	if printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
		minor=${version%.*}
		printf '%s\n%s\n%s\n' "$version" "$minor" "${minor%.*}"
	else
		printf '%s\n' "$version"
	fi
else
	printf 'latest\n%s\n' "$(printf '%s' "$CI_COMMIT_SHA" | cut -c1-8)"
fi > .tags

cat .tags
