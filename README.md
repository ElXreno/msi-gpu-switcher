# msi-gpu-switcher

Minimal GPU MUX switcher for MSI laptops on Linux.

> ⚠️ **Use at your own risk.** This tool writes directly to UEFI variables and
> Embedded Controller registers. I take no responsibility for any damage or loss
> caused by its use.

## Tested Hardware

| Laptop | EC Firmware | Backend |
|--------|-------------|---------|
| MSI Alpha 17 C7VG | `17KKIMS1.114` | `legacy` |
| MSI Titan 18 HX Dragon Edition Draco Epic A2WJ | `1824EMS1.509` | `three-mode` (hybrid and discrete) |

> Other MSI models may work if they share the same UEFI variable and EC layout.
> Open an issue with the output of `sudo msi-gpu-switcher status --debug` and
> say whether switching worked.

## Requirements

- Linux with `efivarfs` mounted at `/sys/firmware/efi/efivars`
- `ec_sys` kernel module loaded with `write_support=1`
- `debugfs` mounted at `/sys/kernel/debug`
- Root privileges

## Installation

### NixOS

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    msi-gpu-switcher = {
      url = "github:elxreno/msi-gpu-switcher";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, msi-gpu-switcher, ... }:
    {
      nixosConfigurations."hostname" = nixpkgs.lib.nixosSystem {
        ...
        modules = [
          ({ pkgs, ... }: {
            environment.systemPackages = [
              msi-gpu-switcher.packages.${pkgs.stdenv.hostPlatform.system}.default
            ];

            # Required for EC writes
            boot.kernelModules = [ "ec_sys" ];
            boot.extraModprobeConfig = "options ec_sys write_support=1";
          })
        ];
      };
    };
}
```

### Manual build

```console
go build -o msi-gpu-switcher .
```

## Usage

```console
  msi-gpu-switcher [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  dgpu        Switch to discrete (dGPU only)
  help        Help about any command
  hybrid      Switch to hybrid (iGPU drives the display, dGPU on demand)
  integrated  Switch to integrated (iGPU only, dGPU off)
  status      Show current GPU/MUX/UEFI status

Flags:
      --debug   enable debug logging
  -h, --help    help for msi-gpu-switcher
```

`igpu` still works as an alias for `hybrid`, `discrete` for `dgpu` and `uma` for
`integrated`. `integrated` is only offered where the firmware supports it.

> **A reboot is required after switching.**

## Troubleshooting

**UEFI variable is immutable:**
```console
chattr -i /sys/firmware/efi/efivars/MsiDCVarData-DD96BAAF-145E-4F56-B1CF-193256298E99
```
The tool attempts this automatically via `FS_IOC_SETFLAGS`; run manually if it fails.

**EC writes fail — reload `ec_sys` with write support:**
```console
modprobe -r ec_sys && modprobe ec_sys write_support=1
```

**`ec0` not found — mount debugfs:**
```console
mount -t debugfs none /sys/kernel/debug
```

## How it works

<details>
<summary>Low-level details</summary>

The firmware applies the GPU mode at boot. The tool leaves it a request through
the UEFI variable `MsiDCVarData` (GUID `DD96BAAF-145E-4F56-B1CF-193256298E99`)
and the EC, and picks one of two backends depending on what the firmware
reports. `status` shows which one is in use.

**`three-mode`** is used when bit 4 of data byte 5 of `MsiDCVarData` is
set, which is how MSI Center recognises firmware with the three-mode switch.
It does what MSI Center does:
- Writes the requested mode (0 hybrid, 1 discrete, 2 integrated) into bits 0-1
  of data byte 5, leaving the rest of the variable as it was. Bits 2-3 hold the
  mode applied at boot, bit 5 says integrated mode is supported and a set bit 6
  means discrete mode is not.
- Sets bit 0 of EC register `0xD1`. The EC raises ACPI query `_Q90`, which
  triggers software SMI `0x11`, and the firmware stores the request.
- Waits for the firmware to acknowledge it with bit 1 of `0xD1`.

**`legacy`** is the original path for older firmware, such as the Alpha 17:
- Writes data byte 1 of `MsiDCVarData`
- Triggers the EC switch (`0xD1`)
- Toggles the EC MUX bit (`0x2E`, mask `0x40`)

The layout of data byte 5 and the SMI it ends in were first worked out in
[ege-adam/msi-gpu-mode](https://github.com/ege-adam/msi-gpu-mode).

</details>

## Notes

- Written with the help of AI.
