import 'dart:convert';

import 'package:fl_clash/common/common.dart';
import 'package:fl_clash/features/form/form.dart';
import 'package:fl_clash/plugins/app.dart';
import 'package:fl_clash/providers/providers.dart';
import 'package:fl_clash/ui/ui.dart';
import 'package:fl_clash/ui/icons/icons.dart';
import 'package:fl_clash/widgets/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:material_ui/material_ui.dart';
import 'package:wifi_ssid/wifi_ssid.dart';

class HomeWifiView extends ConsumerStatefulWidget {
  const HomeWifiView({super.key});

  @override
  ConsumerState<HomeWifiView> createState() => _HomeWifiViewState();
}

class _HomeWifiViewState extends ConsumerState<HomeWifiView> {
  bool _busy = false;
  bool _failed = false;

  Future<void> _perform(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _failed = false;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) setState(() => _failed = true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _editSsid() async {
    final state = ref.read(homeWifiProvider);
    final l = context.appLocalizations;
    final ssid = await dialogs.showCommonDialog<String>(
      child: InputDialog(
        title: l.homeWifiSsid,
        value: state.ssid,
        maxLength: 32,
        validator: (value) =>
            value == null ||
                value.isEmpty ||
                const Utf8Encoder().convert(value).length > 32
            ? l.homeWifiSsidError
            : null,
      ),
    );
    if (ssid == null || !mounted) return;
    await _perform(
      () => ref.read(homeWifiProvider.notifier).configure(state.enabled, ssid),
    );
  }

  Future<void> _authorize() async {
    await _perform(() async {
      final permission = ref.read(locationPermissionsProvider);
      if (permission == WifiSsidPermission.permanentlyDenied) {
        await app?.openAppSettings();
      } else {
        await ref.read(locationPermissionsProvider.notifier).request();
      }
      await ref.read(homeWifiProvider.notifier).refresh();
    });
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(homeWifiProvider);
    final l = context.appLocalizations;
    return CommonScaffold(
      title: l.homeWifiAutomatic,
      body: ListView(
        padding: const EdgeInsets.symmetric(horizontal: 16).copyWith(
          top: context.sectionsTopPadding,
          bottom: 20 + context.bottomInset,
        ),
        children: [
          ErrorBanner(message: _failed ? l.homeWifiError : null),
          generateSection(
            items: [
              ListItem(
                title: Text(l.homeWifiAutomatic),
                trailing: CommonSwitch(
                  value: state.enabled,
                  onChanged: _busy || state.ssid.isEmpty
                      ? null
                      : (value) => _perform(
                          () => ref
                              .read(homeWifiProvider.notifier)
                              .configure(value, state.ssid),
                        ),
                ),
              ),
              ListItem(
                title: Text(l.homeWifiSsid),
                subtitle: Text(state.ssid.isEmpty ? l.addSsid : state.ssid),
                onTap: _busy ? null : _editSsid,
                trailing: const GlyphIcon(AppGlyphs.edit),
              ),
              if (state.enabled) ...[
                ListItem(
                  title: Text(l.outboundMode),
                  subtitle: Text(state.mode.label),
                ),
                ListItem(
                  title: const Text('SSID'),
                  subtitle: Text(state.observedSsid ?? l.homeWifiUnknown),
                ),
              ],
            ],
            footer: l.homeWifiDescription,
          ),
          generateSection(
            items: [
              ListItem(
                title: Text(l.locationPermission),
                subtitle: Text(
                  state.authorized ? l.authorized : l.tapToAuthorize,
                ),
                onTap: _busy ? null : _authorize,
              ),
            ],
            footer: l.locationPermissionDesc,
          ),
          if (_busy) const Center(child: CommonCircleLoading(size: 24)),
        ],
      ),
    );
  }
}
