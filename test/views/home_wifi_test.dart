import 'package:fl_clash/providers/providers.dart';
import 'package:fl_clash/views/home_wifi.dart';
import 'package:fl_clash/widgets/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';

import '../helpers/test_app.dart';

class _HomeWifi extends HomeWifi {
  static final changes = <(bool, String)>[];

  @override
  HomeWifiState build() => const HomeWifiState(ssid: 'Home');

  @override
  Future<void> configure(bool enabled, String ssid) async {
    changes.add((enabled, ssid));
    state = HomeWifiState(enabled: enabled, ssid: ssid);
  }
}

void main() {
  testWidgets('automatic mode can be enabled without stopping VPN', (
    tester,
  ) async {
    _HomeWifi.changes.clear();
    await tester.pumpWidget(
      TestApp(
        locale: const Locale('en'),
        overrides: [
          homeWifiProvider.overrideWith(_HomeWifi.new),
          runTimeProvider.overrideWithBuild(
            (_, _) => DateTime(2026).millisecondsSinceEpoch,
          ),
        ],
        child: const HomeWifiView(),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byType(CommonSwitch));
    await tester.pumpAndSettle();
    expect(_HomeWifi.changes, [(true, 'Home')]);
    expect(
      find.text('Wi-Fi SSID unavailable; using Rule mode'),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });
}
