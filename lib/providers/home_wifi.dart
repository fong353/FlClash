import 'package:fl_clash/enum/enum.dart';
import 'package:fl_clash/plugins/service.dart';
import 'package:fl_clash/providers/app.dart';
import 'package:fl_clash/providers/config.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class HomeWifiState {
  const HomeWifiState({
    this.enabled = false,
    this.ssid = '',
    this.observedSsid,
    this.mode = Mode.rule,
    this.authorized = false,
    this.revision = -1,
  });

  final bool enabled;
  final String ssid;
  final String? observedSsid;
  final Mode mode;
  final bool authorized;
  final int revision;

  factory HomeWifiState.fromMap(Map<Object?, Object?> data) => HomeWifiState(
    enabled: data['enabled'] == true,
    ssid: data['ssid'] as String? ?? '',
    observedSsid: data['observedSsid'] as String?,
    mode: data['mode'] == 'direct' ? Mode.direct : Mode.rule,
    authorized: data['authorized'] == true,
    revision: data['revision'] as int? ?? -1,
  );
}

final homeWifiProvider = NotifierProvider<HomeWifi, HomeWifiState>(
  HomeWifi.new,
);

class HomeWifi extends Notifier<HomeWifiState> with ServiceListener {
  @override
  HomeWifiState build() {
    service?.addListener(this);
    ref.onDispose(() => service?.removeListener(this));
    return const HomeWifiState();
  }

  @override
  void onHomeWifiChanged(Map<Object?, Object?> state) {
    if (!ref.mounted) return;
    final next = HomeWifiState.fromMap(state);
    if (next.revision < this.state.revision) return;
    final wasEnabled = this.state.enabled;
    this.state = next;
    if (next.enabled || wasEnabled) {
      ref
          .read(patchClashConfigProvider.notifier)
          .update((value) => value.copyWith(mode: next.mode));
    }
  }

  Future<void> refresh() async {
    if (ref.read(safeModeProvider)) return;
    final data = await service?.getHomeWifi();
    if (data != null) onHomeWifiChanged(data);
  }

  Future<void> configure(bool enabled, String ssid) async {
    if (ref.read(safeModeProvider)) throw StateError('Safe mode');
    final data = await service?.configureHomeWifi(enabled, ssid);
    if (data == null || data.containsKey('error')) {
      throw StateError('Unable to save home Wi-Fi settings');
    }
    onHomeWifiChanged(data);
  }
}
