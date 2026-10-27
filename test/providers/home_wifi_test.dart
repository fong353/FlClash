import 'package:fl_clash/enum/enum.dart';
import 'package:fl_clash/providers/providers.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:riverpod/riverpod.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('native policy mirrors mode and ignores older replies', () {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final subscription = container.listen(patchClashConfigProvider, (_, _) {});
    addTearDown(subscription.close);
    final notifier = container.read(homeWifiProvider.notifier);
    notifier.onHomeWifiChanged({
      'enabled': true,
      'ssid': 'Home',
      'mode': 'direct',
      'revision': 4,
    });
    expect(container.read(patchClashConfigProvider).mode, Mode.direct);
    container.read(excludeSSIDsProvider.notifier).value = ['Home'];
    container.read(currentSSIDProvider.notifier).value = 'Home';
    expect(container.read(suspendProvider), isFalse);
    container.read(setupActionProvider.notifier).changeMode(Mode.global);
    expect(container.read(patchClashConfigProvider).mode, Mode.direct);
    notifier.onHomeWifiChanged({
      'enabled': true,
      'mode': 'rule',
      'revision': 3,
    });
    expect(container.read(patchClashConfigProvider).mode, Mode.direct);
    notifier.onHomeWifiChanged({
      'enabled': true,
      'mode': 'rule',
      'revision': 5,
    });
    expect(container.read(patchClashConfigProvider).mode, Mode.rule);
    notifier.onHomeWifiChanged({'enabled': false, 'revision': 6});
    container.read(setupActionProvider.notifier).changeMode(Mode.direct);
    expect(container.read(patchClashConfigProvider).mode, Mode.direct);
  });

  test('disabled policy leaves manual mode untouched on refresh', () {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final subscription = container.listen(patchClashConfigProvider, (_, _) {});
    addTearDown(subscription.close);
    container.read(setupActionProvider.notifier).changeMode(Mode.direct);
    container.read(homeWifiProvider.notifier).onHomeWifiChanged({
      'enabled': false,
      'revision': 1,
    });
    expect(container.read(patchClashConfigProvider).mode, Mode.direct);
  });

  test('safe mode does not start native SSID observation', () async {
    final container = ProviderContainer(
      overrides: [safeModeProvider.overrideWithValue(true)],
    );
    addTearDown(container.dispose);
    final notifier = container.read(homeWifiProvider.notifier);
    await notifier.refresh();
    expect(container.read(homeWifiProvider).revision, -1);
    await expectLater(notifier.configure(true, 'Home'), throwsStateError);
  });
}
