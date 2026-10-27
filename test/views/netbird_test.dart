import 'package:fl_clash/core/controller.dart';
import 'package:fl_clash/core/interface.dart';
import 'package:fl_clash/providers/providers.dart';
import 'package:fl_clash/views/netbird.dart';
import 'package:fl_clash/ui/icons/icons.dart';
import 'package:fl_clash/features/form/form.dart';
import 'package:material_ui/material_ui.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mocktail/mocktail.dart';

import '../helpers/glyph_finders.dart';
import '../helpers/test_app.dart';

class _Core extends Mock implements CoreHandlerInterface {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> mount(WidgetTester tester, _Core core) async {
    await tester.pumpWidget(
      TestApp(
        locale: const Locale('en'),
        overrides: [
          coreHandlerProvider.overrideWithValue(CoreController.scoped(core)),
        ],
        child: const NetBirdView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  Map<String, dynamic> status() => {
    'enabled': true,
    'managementUrl': 'https://netbird.example.com',
    'deviceName': 'android-test',
    'cidr': '10.10.10.0/24',
    'state': 'connected',
    'management': true,
    'enrolled': false,
    'ip': '10.10.10.50',
    'peers': [
      {
        'name': 'p2p-device',
        'ip': '10.10.10.2',
        'connected': true,
        'relayed': false,
      },
      {
        'name': 'relay-device',
        'ip': '10.10.10.3',
        'connected': true,
        'relayed': true,
      },
    ],
  };

  testWidgets('shows overlay address and distinct direct and relay states', (
    tester,
  ) async {
    final core = _Core();
    when(() => core.getNetBirdStatus()).thenAnswer((_) async => status());
    await mount(tester, core);
    expect(find.text('10.10.10.50'), findsOneWidget);
    expect(find.textContaining('Direct connection'), findsOneWidget);
    expect(find.textContaining('Relayed connection'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 4));
    verify(() => core.getNetBirdStatus()).called(1);
  });

  testWidgets('a failed status query can recover on refresh', (tester) async {
    final core = _Core();
    when(() => core.getNetBirdStatus()).thenThrow(StateError('offline'));
    await mount(tester, core);
    expect(find.text('Unable to read NetBird status.'), findsOneWidget);
    when(() => core.getNetBirdStatus()).thenAnswer((_) async => status());
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
    expect(find.text('10.10.10.50'), findsOneWidget);
    expect(find.text('Unable to read NetBird status.'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('failed save clears progress and leaves the form editable', (
    tester,
  ) async {
    final core = _Core();
    when(() => core.getNetBirdStatus()).thenAnswer((_) async => status());
    when(() => core.configureNetBird(any())).thenThrow(StateError('rejected'));
    await mount(tester, core);
    await tester.tap(find.text('NetBird configuration'));
    await tester.pumpAndSettle();
    await tester.tap(find.byGlyph(AppGlyphs.check));
    await tester.pumpAndSettle();
    expect(find.textContaining('Unable to save.'), findsOneWidget);
    expect(tester.widget<FormPage>(find.byType(FormPage)).saving, isFalse);
    await tester.pumpWidget(const SizedBox());
  });
}
