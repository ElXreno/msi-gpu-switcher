# msi-gpu-switcher

Minimal GPU MUX switcher for MSI laptops on Linux.

> ⚠️ **Use at your own risk.** This tool writes directly to UEFI variables and
> Embedded Controller registers. I take no responsibility for any damage or loss
> caused by its use.

## Tested Hardware

| Laptop | EC Firmware |
|--------|-------------|
| MSI Alpha 17 C7VG | `17KKIMS1.114` |

> Other MSI models may work if they share the same UEFI variable and EC layout.
> Open an issue with your model and firmware version if it works or fails.

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
  gpu-switcher [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  dgpu        Switch to dGPU (discrete)
  help        Help about any command
  igpu        Switch to iGPU (hybrid)
  status      Show current GPU/MUX/UEFI status

Flags:
      --debug   enable debug logging
  -h, --help    help for gpu-switcher
```

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

Switches the primary GPU output by:
- Writing the UEFI variable `MsiDCVarData` (GUID `DD96BAAF-145E-4F56-B1CF-193256298E99`)
- Triggering the EC switch (`0xD1`)
- Toggling the EC MUX bit (`0x2E`, mask `0x40`)

</details>

## Notes

- Written with the help of AI.
