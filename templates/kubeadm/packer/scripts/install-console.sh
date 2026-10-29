#!/usr/bin/env bash
#
# Give the node a face: a Vates Plymouth splash at boot, then the vates-console
# dashboard on tty1 (node state, Kubernetes version, Linux and Kubernetes
# distributions, pods, events), which is what the Xen Orchestra console shows.
# Run by Packer before install-k8s.sh (see ../almalinux-10-k8s.pkr.hcl), from
# the files the Makefile staged in console/ and Packer uploaded to /tmp/console.
#
# SSH, the other ttys (Alt+F2..F6) and the serial console are left as they are:
# only tty1's login prompt is replaced.
set -e

SRC=/tmp/console

echo "=== 1. Installing the console's dependencies ==="
# libdrm: the dashboard draws through it (dlopen, no cgo). kbd: the console font
# of the text fallback. python3 unzips Poppins.
sudo dnf install -y libdrm kbd python3 \
  dejavu-sans-fonts dejavu-sans-mono-fonts \
  plymouth plymouth-plugin-two-step plymouth-theme-spinner

echo "=== 2. Installing vates-console ==="
sudo install -m 0755 "${SRC}/bin/vates-console" /usr/local/bin/vates-console
sudo install -d /usr/share/vates/assets
sudo install -m 0644 "${SRC}"/bin/assets/*.png /usr/share/vates/assets/
/usr/local/bin/vates-console --version

# The dashboard looks fonts up as dejavu/DejaVuSans.ttf; Alma ships them under
# other directories. Linked into a directory of their own, named by
# VATES_FONT_DIR in the unit, rather than into /usr/share/fonts.
sudo install -d /usr/share/vates/fonts/dejavu
for f in DejaVuSans DejaVuSans-Bold DejaVuSansMono; do
  sudo ln -sf "$(find /usr/share/fonts -name "${f}.ttf" | head -1)" /usr/share/vates/fonts/dejavu/
done

# Poppins, the Vates typeface, where the dashboard looks for it. Same pins as
# vates-kube-os (build/Containerfile).
POPPINS_URL=https://raw.githubusercontent.com/itfoundry/Poppins/v4.003/products/Poppins-4.003-GoogleFonts-TTF.zip
POPPINS_SHA256=69cdf3e8bacecc99fd420186d26dd5769c004ee218034457b3691a1cf408967c
POPPINS_VATES_URL=https://cdn.vates.tech/fonts/poppins_vates/poppins_vates-webfont.ttf
POPPINS_VATES_SHA256=435b8b0807b4f07e92ab72bafdbb57d8e19194951083fd919822ebd24365fc57
tmp=$(mktemp -d)
curl -fsSL --retry 3 -o "${tmp}/poppins.zip" "${POPPINS_URL}"
echo "${POPPINS_SHA256}  ${tmp}/poppins.zip" | sha256sum -c --quiet -
curl -fsSL --retry 3 -o "${tmp}/poppins_vates-webfont.ttf" "${POPPINS_VATES_URL}"
echo "${POPPINS_VATES_SHA256}  ${tmp}/poppins_vates-webfont.ttf" | sha256sum -c --quiet -
python3 -m zipfile -e "${tmp}/poppins.zip" "${tmp}/poppins"
sudo install -d /usr/share/fonts/poppins /usr/share/fonts/poppins-vates
find "${tmp}/poppins" -name '*.ttf' -exec sudo install -m 0644 {} /usr/share/fonts/poppins/ \;
sudo install -m 0644 "${tmp}/poppins_vates-webfont.ttf" /usr/share/fonts/poppins-vates/
rm -rf "${tmp}"

# The text fallback (no DRM card) loads eurlatgr from Debian's path.
[ -e /usr/share/consolefonts ] || sudo ln -s /usr/lib/kbd/consolefonts /usr/share/consolefonts

echo "=== 3. Putting the dashboard on tty1 ==="
sudo install -m 0644 "${SRC}/vates-console.service" /etc/systemd/system/vates-console.service
sudo systemctl mask getty@tty1.service
sudo systemctl enable vates-console.service

echo "=== 4. Installing the Plymouth theme ==="
sudo install -d /usr/share/plymouth/themes/vates
sudo cp /usr/share/plymouth/themes/spinner/*.png /usr/share/plymouth/themes/vates/
sudo install -m 0644 "${SRC}/plymouth/vates.plymouth" "${SRC}/plymouth/watermark.png" \
  /usr/share/plymouth/themes/vates/
sudo plymouth-set-default-theme vates
sudo dracut -f --regenerate-all

# Plymouth draws on the LAST console= of the command line, and the cloud image
# puts the serial port last: the splash would be text on ttyS0, not a picture in
# XO. Keep the serial console, put tty0 last, and ask for the splash. Any serial
# console also drops Plymouth to its text log on every screen, hence
# plymouth.ignore-serial-consoles.
args=$(sudo grubby --info=DEFAULT | sed -n 's/^args="\(.*\)"$/\1/p')
consoles=$(printf '%s\n' ${args} | grep '^console=' | tr '\n' ' ' || true)
serial=$(printf '%s\n' ${args} | grep '^console=ttyS' | tail -1 || true)
[ -n "${consoles}" ] && sudo grubby --update-kernel=ALL --remove-args="${consoles}"
sudo grubby --update-kernel=ALL --args="${serial} console=tty0 rhgb quiet plymouth.ignore-serial-consoles"
sudo grubby --info=DEFAULT | grep '^args='

rm -rf "${SRC}"
