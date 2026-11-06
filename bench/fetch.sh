#!/usr/bin/env bash
# Checks out the repositories bench/run.mjs measures, at the commits it measured.
#   bench/fetch.sh [dir]     (default: bench/repos, which git ignores)
set -euo pipefail
dir=${1:-"$(dirname "$0")/repos"}
mkdir -p "$dir" && cd "$dir"

get() { # url commit dir
  if [ ! -d "$3" ]; then git clone --quiet --filter=blob:none "$1" "$3"; fi
  git -C "$3" checkout --quiet "$2"
}
get https://github.com/documenso/documenso fdd0337 documenso
get https://github.com/lujakob/nestjs-realworld-example-app c1c2cc4 nest-realworld
get https://github.com/gothinkster/node-express-realworld-example-app 30b68e1 .rw-backend
get https://github.com/khaledosman/react-redux-realworld-example-app 53b0b4c .rw-frontend

# The two RealWorld halves in one repository, the way a team keeps them: weave
# checks the front end's calls against the back end's routes.
if [ ! -d realworld ]; then
  mkdir realworld
  git -C .rw-backend archive HEAD | (mkdir -p realworld/backend && tar -x -C realworld/backend)
  git -C .rw-frontend archive HEAD | (mkdir -p realworld/frontend && tar -x -C realworld/frontend)
  printf '.weave/\nnode_modules/\n' > realworld/.gitignore
  git -C realworld init --quiet
  git -C realworld add -A
  git -C realworld -c user.name=bench -c user.email=bench@localhost commit --quiet -m "RealWorld: Express back end + React front end"
fi
echo "repositories in $dir"
