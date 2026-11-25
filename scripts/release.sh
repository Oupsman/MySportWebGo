#!/usr/bin/env bash
# Releases a version of MySportWeb: tags the backend (this repository) and the frontend with the same vX.Y.Z,
# then pushes both tags. The CI of each repository tests the tagged commit and publishes its image X.Y.Z (and
# X.Y, X); deploy it by setting MSW_TAG=X.Y.Z in the stack.
#
#   scripts/release.sh 1.4.0
#
# FRONTEND: the frontend repository, ../mysportweb-vuetify next to this one by default (as MSW_FRONTEND_PATH).
# Both repositories must be on their default branch, without uncommitted changes to tracked files, and
# pushed: the tag must be on a commit the CI has. Nothing is tagged unless both pass, and it asks first.
set -euo pipefail
FRONTEND=/home/oupsman/WebstormProjects/mysportweb-vuetify

version=${1:-}
if ! [[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  # the comment above, as the help
  sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
  exit 2
fi
tag=v$version
backend=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
frontend=${FRONTEND:-$backend/../mysportweb-vuetify}

fail() { echo "ERROR: $*" >&2; exit 1; }

# the latest version tag of a repository, none without
latest() { git -C "$1" tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1 | grep . || echo none; }

# check verifies that a repository can be tagged
check() {
  local repo=$1 name=$2 branch default
  git -C "$repo" rev-parse --git-dir > /dev/null 2>&1 || fail "$name: $repo isn't a git repository (FRONTEND=...)"
  git -C "$repo" fetch --quiet --tags origin
  default=$(git -C "$repo" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2> /dev/null || echo origin/main)
  default=${default#origin/}
  branch=$(git -C "$repo" symbolic-ref --quiet --short HEAD || echo "a detached HEAD")
  [ "$branch" = "$default" ] || fail "$name: on $branch, not $default"
  [ -z "$(git -C "$repo" status --porcelain --untracked-files=no)" ] || fail "$name: uncommitted changes"
  [ "$(git -C "$repo" rev-parse HEAD)" = "$(git -C "$repo" rev-parse "origin/$default")" ] ||
    fail "$name: $default differs from origin/$default: push (or pull) first"
  ! git -C "$repo" rev-parse --quiet --verify "refs/tags/$tag" > /dev/null || fail "$name: $tag exists already"
  # the new version after the latest one
  local previous
  previous=$(latest "$repo")
  if [ "$previous" != none ] && [ "$(printf '%s\n%s\n' "$previous" "$tag" | sort -V | tail -n 1)" != "$tag" ]; then
    fail "$name: $tag isn't after the latest version, $previous"
  fi
}

check "$backend" backend
check "$frontend" frontend

echo "Release $tag:"
for repo in "$backend" "$frontend"; do
  previous=$(latest "$repo")
  if [ "$previous" = none ]; then
    since="first release"
  else
    since="$(git -C "$repo" rev-list --count "$previous..HEAD") commits since $previous"
  fi
  printf '  %-40s %s, %s\n' "$(basename "$(realpath "$repo")")" "$(git -C "$repo" log -1 --format='%h %s' | cut -c1-50)" "$since"
done
if [ "$(latest "$backend")" != "$(latest "$frontend")" ]; then
  echo "WARNING: the repositories had different latest versions: $(latest "$backend") and $(latest "$frontend")"
fi
read -r -p "Tag both and push the tags? [y/N] " answer
[ "$answer" = y ] || [ "$answer" = Y ] || { echo "nothing done"; exit 1; }

for repo in "$backend" "$frontend"; do
  git -C "$repo" tag --annotate "$tag" --message "MySportWeb $version"
done
for repo in "$backend" "$frontend"; do
  git -C "$repo" push --quiet origin "$tag" || fail "pushing $tag from $repo: push it by hand (git push origin $tag)"
done
echo "Pushed $tag. Once both pipelines published their images: MSW_TAG=$version in the stack (see Upgrades in INSTALL.md)."
