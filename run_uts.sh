#!/bin/bash -e
DIR="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

# Build DANM container images and keep the builder container. Use the cache, which is especially
# useful in a pipeline case where we may just have built the image a moment ago.
USE_CACHE=1 KEEP_BUILDER=1 "${DIR}"/build_danm.sh

COMMIT_HASH=$(git rev-parse --short=8 HEAD)
if [ -n "$(git status --porcelain)" ]
then
  COMMIT_HASH="${COMMIT_HASH}_dirty"
fi

echo 'Running DANM UT'
docker run --rm \
  -v ${DIR}/scm/ut/logs:/var/log \
  -v ${DIR}/scm/ut/coverage:/coverage \
  ${TAG_PREFIX}builder:${COMMIT_HASH} \
  scm/ut/run_uts.sh

echo 'DANM tests were successfully executed!'
