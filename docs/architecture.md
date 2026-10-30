# Architecture and recovery

The Python/Tk installer uses Paramiko on Windows and Linux. Device identification
must match the adapter before writes. Backups include hashes and permissions;
they contain private configuration and must not be committed or shared.

The Go ARM controller lives in `/usr/config/paperclip`, outside the small root
partition. The native mining binaries and web API remain installed. Our UI assets
replace the index on the original port 80; the browser talks to the authenticated
local control service on port 8088. CORS permits only the same hostname on the
expected local ports. No remote scripts, analytics, or cloud account are used.
Do not expose these legacy HTTP services directly to the Internet.

`approved.json` is the last confirmed clock/voltage/fan profile. `pending.json`
is written atomically before a trial. At startup the launcher restores the old hardware parameters on disk before
starting the native mining supervisor; the controller then completes API recovery. `halted.json` retains a thermal/fan/telemetry stop across
controller restarts; inspect the miner and use **Resume confirmed settings**.
`network-pending.json` similarly retains the previous network configuration.

An independent supervisor checks the controller heartbeat. On a stall it pauses
the native mining supervisor, stops mining, forces the fans to full speed and
restarts the controller. It does not stop the web service or SSH. Kernel panic
or a complete hardware freeze is a separate failure mode; power-cycle if needed.
No tuning feature depends on leaving a browser or the installer open.

The native driver still expects the name `blake2b(SC)`. Only the user-facing name
is `blake2b(XBT)`. Changing the driver identifier would break algorithm selection.

## If a trial fails

Leave the miner powered and wait for automatic rollback. If the device is fully
unresponsive, power-cycle it once. The previous settings are restored at boot.
If mining remains stopped, inspect cooling and the dashboard fault message before
resuming. Do not delete `pending.json` to force a failed trial through.

## Restoring the original dashboard

Use **Restore a recovery backup** in the installer. It checks the device MAC, SSH
host key, and backup checksums before restoring and restarting the miner.

For manual recovery, keep your backup directory. Restore its `root/yottamc/index.html` and
`etc/inittab` to the matching device paths with their recorded permissions. Remove
the Paperclip startup hook only after restoring the original startup entry. Then
restart the device. The installer never modifies the bootloader or full firmware.
Do not restore a backup from another miner. For a full settings rollback, restore
the matching backed-up configuration files as well; the manifest records them.

## Boundaries

The new controls protect changes made through Paperclip. Direct SSH, the legacy
maintenance interface, and native API clients can bypass them. Avoid using those
interfaces concurrently for tuning. Wi-Fi controls are conditional on native
hardware support. Hardware testing presently covers one original KDBox test unit;
do not infer support for other Goldshell products from the shared UI.
