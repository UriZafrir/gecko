#!/bin/sh
# Builds a signed apt repository from .deb files, ready to serve as static
# files (GitHub Pages: .github/workflows/linux-packages.yml, job apt-pages):
#
#   packaging/linux/apt-repo.sh <dir with .debs> <out dir> <gpg key id>
#
#   <out>/pool/main/g/gecko/*.deb
#   <out>/dists/stable/main/binary-{amd64,arm64}/Packages(.gz)
#   <out>/dists/stable/{Release,InRelease,Release.gpg}
#   <out>/gecko-archive-keyring.gpg   the public key, for signed-by
#   <out>/index.html                  how to add the repository
#
# The key must be in the gpg keyring already (CI imports it from the
# APT_SIGNING_KEY secret). Needs apt-ftparchive (apt-utils) and gpg.
set -eu
debs=${1:?usage: apt-repo.sh <debs> <out> <key id>}
out=${2:?}
key=${3:?}
url=${APT_REPO_URL:-https://eternaleclipse.github.io/gecko}
mkdir -p "$out/pool/main/g/gecko"
cp "$debs"/*.deb "$out/pool/main/g/gecko/"
cd "$out"
for arch in amd64 arm64; do
	d=dists/stable/main/binary-$arch
	mkdir -p "$d"
	apt-ftparchive --arch "$arch" packages pool >"$d/Packages"
	gzip -9nkf "$d/Packages"
done
apt-ftparchive \
	-o APT::FTPArchive::Release::Origin=Gecko \
	-o APT::FTPArchive::Release::Label=Gecko \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Codename=stable \
	-o APT::FTPArchive::Release::Architectures="amd64 arm64" \
	-o APT::FTPArchive::Release::Components=main \
	-o APT::FTPArchive::Release::Description="Gecko packages" \
	release dists/stable >dists/stable/Release
gpg --batch --yes --local-user "$key" --clearsign -o dists/stable/InRelease dists/stable/Release
gpg --batch --yes --local-user "$key" -abs -o dists/stable/Release.gpg dists/stable/Release
gpg --batch --yes --export "$key" >gecko-archive-keyring.gpg
cat >index.html <<HTML
<!doctype html>
<meta charset="utf-8">
<title>Gecko apt repository</title>
<h1>Gecko apt repository</h1>
<p>For Debian 12+ and Ubuntu 22.04+ (amd64, arm64):</p>
<pre>
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL $url/gecko-archive-keyring.gpg | sudo tee /etc/apt/keyrings/gecko.gpg &gt;/dev/null
echo "deb [signed-by=/etc/apt/keyrings/gecko.gpg] $url stable main" | sudo tee /etc/apt/sources.list.d/gecko.list
sudo apt update &amp;&amp; sudo apt install gecko
</pre>
<p><code>gecko</code> is the CLI and daemon; it recommends <code>gecko-desktop</code>, the native window.</p>
HTML
echo "apt repository in $out"
