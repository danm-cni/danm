#!/bin/bash -e
#ERR pseudo-signal is only supported by bash.

#
# Build script to create DANM container images.
#
# This script supports the following environment variables:
#
# - `TAG_PREFIX`. A string that will be prepended to all
#   built image tags. This can, for example, be a registry
#   name. Note that the string is prepended without any additional
#   separators, so if it is eg. a registry name, it MUST end with
#   a "/".
#
# - `EXTRA_BUILD_ARGS`. Any additional arguments that you want to
#   provide to the container build.
#
# - `USE_CACHE`. If defined, use cache during image build process.
#   For compativility with earlier versions of the build process,
#   the default is to NOT use the cache.
#
# - `KEEP_BUILDER`. If defined, keep the builder image and tag
#   it. This can be useful if the image is to be re-used for
#   unit testing, or if a developer wants to run an instance
#   off the builder image as a work environment.
#
# - `IMAGE_PUSH`. If defined, push images to registry right
#   after building.
#

# error handling with trap taken from https://unix.stackexchange.com/questions/79648/how-to-trigger-error-using-trap-command/157327
unset killer_sig
for sig in SIGHUP SIGINT SIGQUIT SIGTERM; do
  trap '
    killer_sig="$sig"
    exit $((128 + $(kill -l "$sig")))' "$sig"
done

trap '
  ret=$?
  [ "$ret" -eq 0 ] || echo >&2 "Terminating with error!"
  if [ -n "$killer_sig" ]; then
    trap - "$killer_sig" # reset traps
    kill -s "$killer_sig" "$$"
  else
    exit "$ret"
  fi' EXIT

trap '
  ret=$?
  error_handler $ret $LINENO $BASH_COMMAND' ERR

error_handler()
{
  echo "$(basename $0) error on line : $2 command was: $3"
  exit $1
}


#
# Don't use cache unless we're told otherwise.
#
if [ -z "${USE_CACHE}" ]
then
  FIRST_BUILD_EXTRA_BUILD_ARGS="--no-cache"
fi


#
# Identify if we need to run docker or buildah
#
if [[ ( "$TRAVIS_PIPELINE" = "buildah" ) || ( "$TRAVIS_PIPELINE" = ""  && -x "$(command -v buildah)" ) ]]
then
  BUILD_COMMAND="buildah bud"
  TAG_COMMAND="buildah tag"
  PUSH_COMMAND="buildah push"
elif [[ ( "$TRAVIS_PIPELINE" = "docker" ) || ( "$TRAVIS_PIPELINE" = ""  && -x "$(command -v docker)" ) ]]
then
  BUILD_COMMAND="docker image build"
  TAG_COMMAND="docker image tag"
  PUSH_COMMAND="docker image push"
else
 echo 'The build process requires docker or buildah/podman installed. Please install any of these and make sure these are executable'
 exit 1
fi


#
# Construct a unique version number from the git commit
# hash that is being build. If the workspace isn't
# clean (ie. if "git status" would say anything other than
# "working tree clean"), add a _dirty suffix to that
# version number.
#
LATEST_TAG=$(git describe --tags)
COMMIT_HASH=$(git rev-parse --short=8 HEAD)
if [ -n "$(git status --porcelain)" ]
then
  COMMIT_HASH="${COMMIT_HASH}_dirty"
fi


#
# Determine which build stages we want to tag as an image.
#
build_targets=(netwatcher svcwatcher webhook danm-cni-plugins)
#build_targets=()

if [ -n "${KEEP_BUILDER}" ]
then
    build_targets+=(builder)
fi

#
# Build the various images. Each image is represented
# by one target in the multi-stage Dockerfile.
#
for plugin in ${build_targets[@]}
do
  echo Building: ${plugin}, version ${COMMIT_HASH}
  ${BUILD_COMMAND} \
    ${EXTRA_BUILD_ARGS} \
    ${FIRST_BUILD_EXTRA_BUILD_ARGS} \
    --build-arg LATEST_TAG=${LATEST_TAG} \
    --build-arg COMMIT_HASH=${COMMIT_HASH} \
    --tag ${TAG_PREFIX}${plugin}:${COMMIT_HASH} \
    --target ${plugin} \
    --file scm/build/Dockerfile \
    .

  # Tag image as "latest", too
  ${TAG_COMMAND} ${TAG_PREFIX}${plugin}:${COMMIT_HASH} ${TAG_PREFIX}${plugin}:latest

  # Push to registry if configured to do so
  if [ -n "${IMAGE_PUSH}" ]
  then
    ${PUSH_COMMAND} ${TAG_PREFIX}${plugin}:${COMMIT_HASH}
    ${PUSH_COMMAND} ${TAG_PREFIX}${plugin}:latest
  fi

  # Make sure we use the cache on the 2nd and subsequent iterations.
  unset FIRST_BUILD_EXTRA_BUILD_ARGS
done
