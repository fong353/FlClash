# Android NetBird integration

This fork keeps FlClash's Flutter UI and Mihomo core, and embeds NetBird in the Android Go library. It does not run the official NetBird application or a second Android VPN service. Android 8.0/API 26 or newer and arm64 are the initial development target.

## Data path

Android applications → FlClash `VpnService` → Mihomo TUN → authenticated loopback SOCKS → embedded NetBird for the configured private IPv4 overlay. Other traffic continues through the selected FlClash profile, including its existing VLESS and Hysteria2 support.

The bridge listens on a random `127.0.0.1` TCP port with a per-process random password. SOCKS CONNECT and UDP ASSOCIATE only accept literal addresses in the configured overlay CIDR. TCP and UDP fail closed while NetBird is disconnected; the bridge never resolves a domain or falls back to an OS socket for an overlay destination. UDP associations are tied to their authenticated TCP connection, pin the local sender, restrict response sources, and expire when idle.

The generated profile receives a private in-memory SOCKS proxy and an initial overlay rule. The Android TUN wrapper also sets the overlay destination's special proxy before Mihomo evaluates rule/global/direct mode. The wrapper preserves Mihomo's provider interface. The bridge password is not written into profile YAML or returned by the NetBird status API. `core/Clash.Meta` is unchanged.

The VPN always includes the overlay route, even when private ranges are otherwise excluded. While NetBird is enabled it does not advertise Android's optional VPN HTTP proxy, so HTTP applications use the same TUN routing as other applications. Per-app VPN exclusions still have their normal meaning: excluded applications do not use either the proxy or NetBird.

## Android networking and lifecycle

NetBird sockets use FlClash's `VpnService.protect()` callback. Physical interface information comes from `ConnectivityManager`/`LinkProperties` through the existing `NetworkObserver`; VPN interfaces are excluded. ICE may establish P2P and otherwise use the control plane's relay configuration. There is no force-relay override.

The Android Go resolver uses the physical DNS servers delivered by `NetworkObserver` and protects its resolver sockets while the VPN runs, avoiding recursive lookups through the virtual DNS server. Mihomo's existing profile DNS implementation remains responsible for application DNS.

The existing `ServiceState` owns Android VPN intent. The embedded module coalesces its own start/stop requests and cancels pending enrollment without blocking the service callback. Stop closes bridge sessions and stops the embedded client. Network changes reach NetBird's connection-event manager. Transient engine state stays in memory because the embedded client does not manage host routes or DNS. Registration failure leaves public proxy traffic available and overlay traffic blocked; stop/start retries enrollment.

## Home Wi-Fi automatic mode

Open **Tools → Home Wi-Fi automatic mode**, enter the exact home SSID and enable the switch. A matching Wi-Fi SSID selects **Direct**; other networks, a missing/redacted SSID, disabled location or missing background location permission select **Rule**. Matching is case sensitive and preserves spaces. The home SSID is a local Android setting, never a source-code default. Android requires precise location, background location and the system location switch for background SSID detection.

The native observer runs independently of the Flutter screen and also starts through Quick Settings. It requests SSID information only while this feature is enabled. The Go mode override survives profile reloads and prevents stale Flutter mode updates from replacing the automatic choice. Starting a new process uses Rule until the home SSID is observed. This changes the current mode without restarting the VPN or NetBird. While enabled it also takes precedence over the older SSID exclusion feature, which would otherwise pause the VPN. Disable the switch to return to manual mode selection; disabling initially selects Rule.

Existing profile DNS and per-app VPN exclusions retain their normal behavior. This feature selects a Mihomo mode and does not configure the home gateway or stop the Android VPN.

Verification includes 66 covering Flutter tests, 99 Android unit tests, Core tests and Android Go vet. On the S23, background Wi-Fi removal selected Rule and reconnection selected Direct, with the same application process and VPN service instance throughout. NetBird returned connected with the same overlay identity. Extended screen-off behavior remains outside this short transition check.

## Settings and identity

Open **Tools → NetBird → NetBird configuration**. Stop the VPN before saving changes, then start it normally. Configure an HTTPS management server, a device name, the private IPv4 overlay CIDR and, for first enrollment, a one-time setup key. Example values in the application are placeholders.

The fork has application ID `com.nate.flclash.netbird` (`.dev` for development builds) and Android display name **FlClash NetBird**. It can coexist with upstream FlClash and the official NetBird client. Only one of those applications can own the VPN for the current Android user at a time.

Settings and a separate NetBird identity are stored beneath Android's private `noBackupFilesDir/netbird/`, outside the document provider's exported files tree, with a stable subdirectory derived from the profile home directory, with directory mode 0700 and files mode 0600. The status RPC omits the setup key. Configuration RPC calls bypass argument logging. After successful enrollment the setup key is removed from settings; subsequent starts use the stored identity. An enrolled identity remains bound to its original management server and device name. No existing system or official-app NetBird identity is read. Android application backups are disabled; FlClash's explicit profile backup does not include these identity files.

This version provides outbound TCP/UDP access using overlay IPv4 addresses. It does not provide overlay host-name resolution, ICMP/ping, incoming access to phone services, subnet routing or exit-node advertisement. Do not treat a connected management session as proof that an arbitrary internal service is reachable.

## Reproducible build

- FlClash base: upstream `v0.9.2`/main with Mihomo submodule commit `c5062921054902b25215a6a933e72ccd714c9bfa`.
- Flutter 3.47.6, Go toolchain 1.27.1, JDK 17 and Android NDK 28.2.13676358.
- NetBird base v0.81.4; dependency fork [fong353/netbird](https://github.com/fong353/netbird/tree/codex/flclash-android-embed), commit `9f69e64ea6777cde8fb14f07399ec71712636be4`, pinned by immutable pseudo-version in `core/go.mod` and `core/go.sum`.

The NetBird patch exposes physical-interface discovery and network events to `client/embed`, honors supplied interface discovery and Android socket protection in netstack mode, and makes enrollment authentication honor the startup context's cancellation. Supplied discovery must take precedence over the netstack fallback: Android applications cannot enumerate interfaces through Go's netlink path. It does not alter control-plane, ICE/relay selection or authorization policy. The Android linker uses `-checklinkname=0`, as the [official NetBird Android build](https://github.com/netbirdio/android-client/blob/main/build-android-lib.sh) does, because its existing `anet` dependency accesses the Go interface zone cache.

```sh
git submodule update --init --recursive
flutter pub get
flutter build apk --debug --target-platform android-arm64
```

The development APK is `build/app/outputs/flutter-apk/app-debug.apk`. Development signing is not the upstream signing identity. Use an explicit device serial and `adb install --user 0` for an authorized S23 installation. Do not install over the official application's package or copy its private identity.

## Private self-use provisioning

To preset enrollment without entering a form, place `netbird-provision.json` in a private directory outside this repository and build with that directory as an additional Android asset source:

```sh
FLCLASH_PRIVATE_ASSETS=/absolute/private/assets flutter build apk --debug --target-platform android-arm64
```

The JSON uses the configuration RPC's fields: `enabled`, `managementUrl`, `deviceName`, `cidr` and the initial one-time `setupKey`. On first Core initialization the native host atomically copies it to private settings with mode 0600. Existing settings take precedence, including after an app update or VPN restart. The key never crosses Flutter's startup arguments, and the private key is generated on the device rather than embedded in the APK. A build without this environment variable has no provisioning asset.

An APK containing a setup key is a private enrollment artifact. Revoke the key at the control plane after registration; the enrolled device continues with its own identity. A fresh installation needs a fresh enrollment key. Do not commit the private asset, upload this APK to public releases, or run both upstream and forked FlClash cores on the same profile ports. Stop the old application before starting the fork, since Android isolates application files but shares TCP/UDP listener ports.

## Verification

```sh
flutter analyze --no-fatal-infos
flutter test test/providers/home_wifi_test.dart test/views/home_wifi_test.dart test/views/netbird_test.dart test/core/protocol_contract_test.dart test/l10n test/lint
cd core
CGO_ENABLED=0 go test .
go test -race ./netbirdbridge
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go vet -tags=with_gvisor . ./mobile ./tun ./platform ./netbirdbridge
```

`android/app/src/androidTest/kotlin/com/follow/clash/NetBirdIntegrationTest.kt` tests the actual native VPN service on an isolated Android emulator. Pre-authorize VPN only in that isolated environment; this is not a script for changing the phone's app policies. The baseline test checks TUN routes, VPN HTTP-proxy suppression, secret redaction and repeated start/stop. The optional live test reads a private fixture from the test application's files directory and deletes it after reading; it checks TCP/UDP source identity in every Mihomo mode, public proxy egress and reconnect with the same peer. Fixtures, keys and real node parameters are never committed.

The arm64 development build was verified on 2026-10-27 with Android API 37 on a dedicated AOSP ATD emulator:

- Flutter analysis reported no issues; the covering Flutter suites passed all 49 tests.
- Core protocol tests, bridge race tests and Android Go vet passed. The pinned NetBird fork also passed its authentication-cancellation regression and Android vet.
- The native service test passed three VPN start/stop cycles, overlay route and HTTP-proxy checks, and cancellation of pending enrollment.
- The live test passed public HTTPS through a VLESS proxy alongside overlay TCP and UDP through the same VPN. The receiver observed the emulator's own NetBird address in rule, global and direct modes. A VPN restart retained that identity.
- The consumed setup key was removed from settings. A document-provider request for the private identity settings was rejected. Runtime logs did not contain the test setup key or proxy credential.
- Both temporary enrollment keys were revoked and both temporary peers deleted after testing. Existing peer identities and authorization, policies, DNS, routes and networks were unchanged. The test applications, emulator and local echo process were removed.

The baseline emulator APK SHA-256 is `3590e62c8a2a68c275cbdf4d0ad1da55c336830ce916c96fe25103a3975740a3`; Android APK signature verification passed. This is a locally signed development artifact, not a published release.

The updated private build was also installed on a Samsung Galaxy S23 running Android API 37. Preset enrollment, retained identity after a key-revoked app update, and overlay TCP/UDP in rule/global/direct mode passed. Public HTTPS passed with both Hysteria2 and VLESS selected. A Wi-Fi → cellular → Wi-Fi check passed public HTTPS and overlay TCP/UDP with the same identity, and the restored Wi-Fi path established P2P. One initial TCP request closed during the first transition; a second full cellular test passed on its first TCP attempt. These checks establish recovery in the observed transitions, not uninterrupted existing TCP sessions. The original applications and their private identities were retained. The real-device check also exposed and fixed the netstack interface-discovery fallback described above; its regression test runs in the dependency fork.

Run Android builds and Flutter tests serially because they share generated assets and build outputs. A follow-up check after one minute also passed public HTTPS and overlay TCP/UDP; continuous screen-off duration was not confirmed. Extended Samsung background behavior and overnight battery use remain separate validation work. Incoming phone services are outside the implemented feature set.
