#!/usr/bin/env bash
# Publishes the version in VERSION from this machine: every check, both images, the tag and the Release.
# Safe to run again after a failure: what is already published is left as it is.
#
#   scripts/publish.sh --build   checks and builds only, publishes nothing
#   scripts/publish.sh
set -euo pipefail
cd "$(dirname "$0")/.."
v="$(tr -d '[:space:]' < VERSION)"; tag="v$v"; image=madrays/trackferry; repo=madrays/trackferry
step() { printf '\n▸ %s\n' "$*"; }

step "核对 $tag"
[ -z "$(git status --porcelain)" ] || { echo "工作区有未提交的改动" >&2; exit 1; }
[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || { echo "请在 main 上发版" >&2; exit 1; }
scripts/check-release.sh "$tag"
go run ./scripts/sign check
for f in compose.yaml compose.bridge.yaml; do docker compose -f "$f" config -q; done
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null && [ "$(git rev-parse "$tag^{commit}")" != "$(git rev-parse HEAD)" ]; then
  echo "$tag 已经指向别的提交，版本号不能重用" >&2; exit 1
fi

step 测试
(cd web && npm ci --no-audit --no-fund && npm test && npm run build)
[ -z "$(gofmt -l cmd internal)" ] || { echo "有文件没有 gofmt" >&2; exit 1; }
go vet ./...
go test -race ./...

step 构建
commit="$(git rev-parse HEAD)"
mkdir -p build/run dist
for ca in /etc/ssl/certs/ca-certificates.crt /etc/ssl/cert.pem; do [ ! -f "$ca" ] || { cp "$ca" dist/ca-certificates.crt; break; }; done
# arm is 32-bit ARMv7 (older NAS and boxes); the image's TARGETARCH for linux/arm/v7 is "arm".
for arch in amd64 arm64 arm; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch GOARM=7 go build -trimpath \
    -ldflags "-s -w -X main.version=$v -X main.commit=${commit:0:7} -X main.repo=$repo" \
    -o dist/$arch/trackferry ./cmd/trackferry
done
bx() {
  docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -f build/Dockerfile.release --provenance=false \
    --label org.opencontainers.image.source="https://github.com/$repo" \
    --label org.opencontainers.image.version="$v" \
    --label org.opencontainers.image.revision="$commit" \
    --label org.opencontainers.image.licenses=MIT "$@" .
}
# The television apps, one per ABI, signed with the key in .private (never in Git).
tv() {
  [ -f .private/trackferry-tv.properties ] || { echo "缺少 .private/trackferry-tv.properties，电视应用无法签名" >&2; exit 1; }
  [ -f android-tv/local.properties ] || [ -n "${ANDROID_HOME:-}" ] || { echo "找不到 Android SDK：设置 ANDROID_HOME 或 android-tv/local.properties" >&2; exit 1; }
  (cd android-tv && ./gradlew --no-daemon -q clean testDebugUnitTest assembleRelease)
  sdk="${ANDROID_HOME:-$(sed -n 's/^sdk.dir=//p' android-tv/local.properties)}"
  signer="$(ls -d "$sdk"/build-tools/*/ | sort -V | tail -1)apksigner"
  for abi in armeabi-v7a arm64-v8a; do
    f="android-tv/app/build/outputs/apk/release/app-$abi-release.apk"
    "$signer" verify "$f" >/dev/null || { echo "$f 没有签名" >&2; exit 1; }
    "$signer" verify --print-certs "$f" | grep -q "CN=TrackFerry TV" || { echo "$f 不是用电视发版密钥签的" >&2; exit 1; }
    cp "$f" "dist/trackferry-tv-$v-android-$abi.apk"
  done
}
step 电视应用
tv
if [ "${1:-}" = --build ]; then bx -t "$image:$v"; exit; fi

step 镜像
git fetch -q origin main
git merge-base --is-ancestor HEAD origin/main || { echo "先推送 main：git push origin main" >&2; exit 1; }
if out="$(docker buildx imagetools inspect "$image:$v" --format '{{json .Image}}' 2>&1)"; then
  grep -q "$commit" <<< "$out" || { echo "$image:$v 已由别的提交发布，版本号不能重用" >&2; exit 1; }
  echo "$image:$v 已在 Docker Hub"
else
  grep -qiE "not found|does not exist|insufficient_scope|denied" <<< "$out" || { echo "$out" >&2; exit 1; }
  bx -t "$image:$v" --push
fi
# Always: an earlier run may have stopped between the image and these.
docker buildx imagetools create -t "$image:${v%.*}" -t "$image:latest" "$image:$v"

step "标签与 Release"
git rev-parse -q --verify "refs/tags/$tag" >/dev/null || scripts/release-notes.sh "$v" | git tag -a "$tag" --cleanup=whitespace -F -
git push origin "$tag"
# What the settings page installs: the binary of each platform with its signature.
assets=(compose.yaml dist/trackferry-homeassistant.zip)
mkdir -p dist
git archive --format=zip -o dist/trackferry-homeassistant.zip HEAD custom_components/trackferry # unpacks into the configuration folder
# Named trackferry-<version>-<os>-<arch>. The same files go up again under the names used before
# 1.9.0 (trackferry-linux-<goarch>, trackferry-tv-<version>-<abi>.apk), which servers of those
# versions ask for; the signature covers the content, so one .sig serves both names.
# internal/update ServerAssets and TVAssets hold the same names.
for arch in amd64 arm64 arm; do
  name=$arch; [ $arch = arm ] && name=armv7
  f="dist/trackferry-$v-linux-$name" old="dist/trackferry-linux-$arch"
  cp "dist/$arch/trackferry" "$f"
  go run ./scripts/sign "$v" "$arch" "$f" > "$f.sig"
  cp "$f" "$old"; cp "$f.sig" "$old.sig"
  assets+=("$f" "$f.sig" "$old" "$old.sig")
done
# The television apps: the server fetches these and checks the signature (internal/update/tv.go).
for abi in armeabi-v7a arm64-v8a; do
  f="dist/trackferry-tv-$v-android-$abi.apk" old="dist/trackferry-tv-$v-$abi.apk"
  go run ./scripts/sign "$v" "android/$abi" "$f" > "$f.sig"
  cp "$f" "$old"; cp "$f.sig" "$old.sig"
  assets+=("$f" "$f.sig" "$old" "$old.sig")
done
if have="$(gh release view "$tag" -R "$repo" --json assets --jq '.assets[].name' 2>/dev/null)"; then
  missing=0
  for f in "${assets[@]}"; do grep -qx "$(basename "$f")" <<< "$have" || missing=1; done
  if [ $missing = 1 ]; then gh release upload "$tag" -R "$repo" --clobber "${assets[@]}"; else echo "Release 已存在"; fi
else
  scripts/release-notes.sh "$v" | gh release create "$tag" -R "$repo" --verify-tag --title "TrackFerry $v" --notes-file - "${assets[@]}"
fi
