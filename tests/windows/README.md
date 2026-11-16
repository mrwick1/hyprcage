# Windows test VM

The Windows port is tested in a Windows 11 Pro VM under QEMU/KVM. `vm.sh` starts, stops and resets it.

## Build the VM once

1. Run `quickget windows 11 "English (United States)"` in `/data/vms`.
2. If Microsoft blocks the scripted download, get the ISO link from the Microsoft download page in a browser. Save the ISO as `windows-11-English-United-States/windows-11.iso`. Check it against the SHA-256 on the same page.
3. If `virtio-win.iso` is an HTML page, download it again with `curl -A curl/8`. The site sends a bot check to browser user agents.
4. Add `setup-ssh.ps1` and the public key `winvm.pub` to `unattended/`. Add a FirstLogonCommand that runs the script. Rebuild `unattended.iso` with `mkisofs -J`. The script installs OpenSSH Server, accepts only the key, and turns off password and keyboard-interactive logins.
5. Copy quickemu's launch script to `launch-stdvga.sh` and replace `-vga none -device qxl-vga,...` with `-vga std`.
6. Start the VM. Press a key at "Press any key to boot from CD" (`sendkey spc` on the QEMU monitor). Answer the language and keyboard pages. The answer file does the rest.
7. Install the tools: `winget install --id GoLang.Go -e`, then `Git.Git` and `Google.Chrome`.
8. Stop the VM. Run `qemu-img snapshot -c clean-base disk.qcow2`. Copy `OVMF_VARS.fd` and `tpm2-00.permall` to `clean-base/`.

## Use it

```
tests/windows/vm.sh start
tests/windows/vm.sh ssh 'go version'
tests/windows/vm.sh stop
tests/windows/vm.sh reset
```

QEMU forwards guest port 22 to host port 22220 on all interfaces. Only key login works.
