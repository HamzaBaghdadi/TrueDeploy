#!/usr/bin/env bash
# Sets the app version everywhere it's recorded. Usage: scripts/bump-version.sh 0.2.0
set -euo pipefail
V="${1:-}"
if ! [[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "usage: $0 <version>   e.g. $0 0.2.0 or $0 0.2.0-rc.1" >&2
  exit 1
fi
cd "$(dirname "$0")/.."
CORE="${V%%-*}" # installers and bundles want plain x.y.z

sed -i.bak "s/^const Version = \".*\"$/const Version = \"$V\"/" services/app.go
sed -i.bak "s/^  version: \"[^\"]*\"/  version: \"$V\"/" build/config.yml
sed -i.bak -E "s/(\"(file_version|product_version|FileVersion|ProductVersion)\": )\"[^\"]*\"/\1\"$CORE\"/" build/windows/info.json
sed -i.bak "s/^version: .*/version: \"$CORE\"/" build/linux/nfpm/nfpm.yaml
for p in build/darwin/Info.plist build/darwin/Info.dev.plist; do
  perl -0pi -e "s#(<key>CFBundle(?:Short)?Version(?:String)?</key>\s*<string>)[^<]*#\${1}$CORE#g" "$p"
done
rm -f services/app.go.bak build/config.yml.bak build/windows/info.json.bak build/linux/nfpm/nfpm.yaml.bak

echo "Version set to $V. Next:"
echo "  git commit -am \"Release $V\" && git tag v$V && git push origin main v$V"
