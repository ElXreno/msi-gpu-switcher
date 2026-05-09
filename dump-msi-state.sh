#!/usr/bin/env bash
set -u

TS="$(date +'%Y-%m-%d_%H-%M-%S')"
OUT="${1:-msi-state-$TS}"

mkdir -p "$OUT"/{system,gpu,kernel,msi-ec,uefi,ec-raw,logs}

run() {
  local name="$1"; shift
  {
    echo "\$ $*"
    echo
    "$@"
  } > "$OUT/$name" 2>&1
}

copy_if_exists() {
  local src="$1"
  local dst="$2"
  if [ -e "$src" ]; then
    cp -a "$src" "$dst" 2>"$OUT/logs/copy-errors.log" || true
  fi
}

echo "Dumping into: $OUT"

# Basic system identity
run system/uname.txt uname -a
run system/os-release.txt cat /etc/os-release
run system/hostnamectl.txt hostnamectl

# DMI / MSI model info
for key in \
  system-manufacturer \
  system-product-name \
  system-version \
  system-serial-number \
  baseboard-manufacturer \
  baseboard-product-name \
  baseboard-version \
  bios-vendor \
  bios-version \
  bios-release-date
do
  run "system/dmidecode-$key.txt" sudo dmidecode -s "$key"
done

# Kernel config
run kernel/cmdline.txt cat /proc/cmdline
run kernel/loaded-modules.txt lsmod
run kernel/modules-ec-search.txt find "/lib/modules/$(uname -r)" -iname '*ec*'
run kernel/config-acpi-ec.txt bash -c "grep -Ei 'ACPI_EC|EC_SYS|DEBUG_FS|MSI_EC' /boot/config-$(uname -r) || true"
run kernel/debugfs-mounts.txt bash -c "mount | grep debugfs || true"

# GPU / PCI state
run gpu/lspci-gpu.txt bash -c "lspci -nnk | grep -A 6 -E '(VGA|3D|Display)'"
run gpu/lspci-full.txt lspci -nnk
run gpu/drm-tree.txt bash -c "find /sys/class/drm -maxdepth 3 -type f -print -exec sh -c 'echo ---; cat \"$1\" 2>/dev/null' sh {} \\;"
run gpu/pci-gpu-sysfs.txt bash -c '
for d in /sys/bus/pci/devices/*; do
  class=$(cat "$d/class" 2>/dev/null || true)
  case "$class" in
    0x0300*|0x0302*|0x0380*)
      echo "== $d =="
      for f in class vendor device subsystem_vendor subsystem_device boot_vga power_state; do
        [ -e "$d/$f" ] && echo "$f=$(cat "$d/$f" 2>/dev/null)"
      done
      [ -L "$d/driver" ] && echo "driver=$(basename "$(readlink "$d/driver")")"
      echo
    ;;
  esac
done'

# msi-ec exposed sysfs state
if [ -d /sys/devices/platform/msi-ec ]; then
  run msi-ec/files.txt find /sys/devices/platform/msi-ec -type f
  run msi-ec/values.txt bash -c "grep -R . /sys/devices/platform/msi-ec 2>/dev/null || true"
  run msi-ec/tree.txt bash -c "find /sys/devices/platform/msi-ec -maxdepth 4 -print"
else
  echo "No /sys/devices/platform/msi-ec" > "$OUT/msi-ec/not-found.txt"
fi

# MSI UEFI variable used by msi-gpu-switcher
UEFI_VAR="/sys/firmware/efi/efivars/MsiDCVarData-DD96BAAF-145E-4F56-B1CF-193256298E99"
if [ -e "$UEFI_VAR" ]; then
  sudo cp "$UEFI_VAR" "$OUT/uefi/MsiDCVarData.raw" 2>"$OUT/logs/uefi-copy-error.txt" || true
  run uefi/MsiDCVarData-stat.txt stat "$UEFI_VAR"
  run uefi/MsiDCVarData-xxd.txt xxd "$UEFI_VAR"
  run uefi/MsiDCVarData-byte-summary.txt bash -c '
    raw="'"$OUT"'/uefi/MsiDCVarData.raw"
    if [ -f "$raw" ]; then
      echo "efivar attrs are first 4 bytes"
      echo "data byte[1] = raw offset 5"
      od -An -tx1 -v "$raw"
    fi'
else
  echo "MsiDCVarData not found" > "$OUT/uefi/not-found.txt"
fi

# msi-gpu-switcher status if binary is nearby or installed
if command -v msi-gpu-switcher >/dev/null 2>&1; then
  run logs/msi-gpu-switcher-status.txt sudo msi-gpu-switcher --debug status
elif [ -x ./msi-gpu-switcher ]; then
  run logs/msi-gpu-switcher-status.txt sudo ./msi-gpu-switcher --debug status
else
  echo "msi-gpu-switcher not found in PATH or current dir" > "$OUT/logs/msi-gpu-switcher-not-found.txt"
fi

# Raw EC dump if ec_sys/debugfs exists
if [ -e /sys/kernel/debug/ec/ec0/io ]; then
  echo "Raw EC available; dumping /sys/kernel/debug/ec/ec0/io"
  sudo cp /sys/kernel/debug/ec/ec0/io "$OUT/ec-raw/ec0-io.bin" 2>"$OUT/logs/ec-raw-copy-error.txt" || true
  run ec-raw/ec0-io-xxd.txt xxd /sys/kernel/debug/ec/ec0/io
  run ec-raw/ec-known-offsets.txt bash -c '
    EC=/sys/kernel/debug/ec/ec0/io
    echo "offset 0x2e suspected MUX byte:"
    sudo dd if="$EC" bs=1 skip=$((0x2e)) count=1 2>/dev/null | xxd
    echo
    echo "offset 0xd1 suspected switch trigger byte:"
    sudo dd if="$EC" bs=1 skip=$((0xd1)) count=1 2>/dev/null | xxd
  '
else
  echo "No /sys/kernel/debug/ec/ec0/io available" > "$OUT/ec-raw/not-available.txt"
fi

# Extra logs
run logs/dmesg-msi-ec-gpu.txt sudo dmesg | grep -Ei 'msi|ec|mux|gpu|nvidia|intel|drm|efi'
run logs/journal-boot-gpu.txt journalctl -b --no-pager | grep -Ei 'msi|ec|mux|gpu|nvidia|intel|drm|efi'

# Archive
tar -czf "$OUT.tar.gz" "$OUT"

echo
echo "Done."
echo "Folder:  $OUT"
echo "Archive: $OUT.tar.gz"
