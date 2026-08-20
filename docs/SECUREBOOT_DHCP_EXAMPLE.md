# Secure Boot DHCP filename contract

AegisPXE does not own DHCP in the current architecture. The DHCP server must distinguish x86-64 UEFI clients and return the AegisPXE TFTP server plus:

```text
ipxe-shim.efi
```

for Secure Boot capable UEFI clients.

The official iPXE shim derives and loads the matching `ipxe.efi` second stage from the same location. Serving `ipxe.efi` directly bypasses the required first-stage shim and is not a valid AegisPXE Secure Boot deployment.

BIOS may continue to use `undionly.kpxe` only when policy permits non-Secure-Boot operation. With the packaged `required` policy, BIOS discovery can register a Machine but destructive provisioning will not be authorized.
