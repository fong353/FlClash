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

The existing `ServiceState` owns Android VPN intent. The embedded module coalesces its own start/stop requests and cancels pending enrollment without blocking the service callback. Stop closes bridge sessions and stops the embedded client. Network changes reach NetBird's connection-event manager. Registration failure leaves public proxy traffic available and overlay traffic blocked; stop/start retries enrollment.

## Settings and identity

Open **Tools → NetBird → NetBird configuration**. Stop the VPN before saving changes, then start it normally. Configure an HTTPS management server, a device name, the private IPv4 overlay CIDR and, for first enrollment, a one-time setup key. Example values in the application are placeholders.

The fork has application ID `com.nate.flclash.netbird` (`.dev` for development builds) and Android display name **FlClash NetBird**. It can coexist with upstream FlClash and the official NetBird client. Only one of those applications can own the VPN for the current Android user at a time.

Settings and a separate NetBird identity are stored beneath Android's private `noBackupFilesDir/netbird/`, outside the document provider's exported files tree, with a stable subdirectory derived from the profile home directory, with directory mode 0700 and files mode 0600. The status RPC omits the setup key. Configuration RPC calls bypass argument logging. After successful enrollment the setup key is removed from settings; subsequent starts use the stored identity. An enrolled identity remains bound to its original management server and device name. No existing system or official-app NetBird identity is read. Android application backups are disabled; FlClash's explicit profile backup does not include these identity files.

This version provides outbound TCP/UDP access using overlay IPv4 addresses. It does not provide overlay host-name resolution, ICMP/ping, incoming access to phone services, subnet routing or exit-node advertisement. Do not treat a connected management session as proof that an arbitrary internal service is reachable.

## Reproducible build

- FlClash base: upstream `v0.9.2`/main with Mihomo submodule commit `c5062921054902b25215a6a933e72ccd714c9bfa`.
- Flutter 3.47.6, Go toolchain 1.27.1, JDK 17 and Android NDK 28.2.13676358.
- NetBird base v0.81.4; dependency fork [fong353/netbird](https://github.com/fong353/netbird/tree/codex/flclash-android-embed), commit `d6ee0f6c3da724f2ab0054831b0b1f56adbaaed6`, pinned by immutable pseudo-version in `core/go.mod` and `core/go.sum`.

The NetBird patch exposes physical-interface discovery and network events to `client/embed`, honors an explicitly installed Android socket protector in netstack mode, and makes enrollment authentication honor the startup context's cancellation. It does not alter control-plane, ICE/relay selection or authorization policy. The Android linker uses `-checklinkname=0`, as the [official NetBird Android build](https://github.com/netbirdio/android-client/blob/main/build-android-lib.sh) does, because its existing `anet` dependency accesses the Go interface zone cache.

```sh
git submodule update --init --recursive
flutter pub get
flutter build apk --debug --target-platform android-arm64
```

The development APK is `build/app/outputs/flutter-apk/app-debug.apk`. Development signing is not the upstream signing identity. Use an explicit device serial and `adb install --user 0` for an authorized S23 installation. Do not install over the official application's package or copy its private identity.

## Verification

```sh
flutter analyze --no-fatal-infos
flutter test test/views/netbird_test.dart test/core/protocol_contract_test.dart test/l10n test/lint
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

The verified APK SHA-256 is `3590e62c8a2a68c275cbdf4d0ad1da55c336830ce916c96fe25103a3975740a3`; Android APK signature verification passed. This is a locally signed development artifact, not a published release. No S23 installation or phone-network change was performed.

Run Android builds and Flutter tests serially because they share generated assets and build outputs. Device-specific Samsung background behavior, real Wi-Fi/cellular switching, overnight battery use and incoming phone services remain separate validation work.
