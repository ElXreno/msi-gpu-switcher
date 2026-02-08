# msi-gpu-switcher

Minimal GPU MUX switcher for MSI laptops.

## What it does
Switches the primary GPU output by:
- Writing the UEFI variable `MsiDCVarData` (GUID `DD96BAAF-145E-4F56-B1CF-193256298E99`)
- Triggering the EC switch (`0xD1`)
- Toggling the EC MUX bit (`0x2E`, mask `0x40`)

## Requirements
- Linux with `efivarfs` mounted at `/sys/firmware/efi/efivars`
- `ec_sys` kernel module (load with `write_support=1`)
- `debugfs` mounted at `/sys/kernel/debug`
- Root privileges for `igpu`/`dgpu`

## NixOS configuration
Minimal flake example:
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

## Build
```console
go build -o msi-gpu-switcher .
```

## Usage
```console
msi-gpu-switcher
Switch primary GPU output using UEFI vars and EC trigger.

Usage:
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

Use "gpu-switcher [command] --help" for more information about a command.
```

## Notes
- This application was written with the help of AI for another person.
- I do not accept any responsibility for any **damage** or **loss** caused by its use.
- Tested on MSI Alpha 17 C7VG (EC firmware: `17KKIMS1.114`).
- The tool tries to temporarily clear the immutable flag via `FS_IOC_SETFLAGS`.
  If it fails, run:
  `chattr -i /sys/firmware/efi/efivars/MsiDCVarData-DD96BAAF-145E-4F56-B1CF-193256298E99`
- For EC writes, load `ec_sys` with `write_support=1`, for example:
  `modprobe -r ec_sys && modprobe ec_sys write_support=1`
- If `ec0` is missing, mount debugfs:
  `mount -t debugfs none /sys/kernel/debug`
- A reboot is required after MUX mode change.
