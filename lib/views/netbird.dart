import 'dart:async';

import 'package:fl_clash/common/common.dart';
import 'package:fl_clash/features/form/form.dart';
import 'package:fl_clash/providers/providers.dart';
import 'package:fl_clash/ui/ui.dart';
import 'package:fl_clash/ui/icons/icons.dart';
import 'package:fl_clash/widgets/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:material_ui/material_ui.dart';

class NetBirdView extends ConsumerStatefulWidget {
  const NetBirdView({super.key});

  @override
  ConsumerState<NetBirdView> createState() => _NetBirdViewState();
}

class _NetBirdViewState extends ConsumerState<NetBirdView> {
  Map<String, dynamic>? _status;
  bool _loadFailed = false;
  Timer? _timer;
  bool _refreshing = false;

  @override
  void initState() {
    super.initState();
    unawaited(_refresh());
    _timer = Timer.periodic(const Duration(seconds: 3), (_) => _refresh());
  }

  Future<void> _refresh() async {
    if (_refreshing) return;
    _refreshing = true;
    try {
      final status = await ref.read(coreHandlerProvider).getNetBirdStatus();
      if (mounted) {
        setState(() {
          _status = status;
          _loadFailed = false;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() => _loadFailed = true);
      }
    } finally {
      _refreshing = false;
    }
  }

  Future<void> _edit() async {
    final status = _status;
    if (status == null) return;
    await showFormSheet<void>(
      context: context,
      builder: (_) => _NetBirdForm(status: status),
    );
    if (mounted) await _refresh();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l = context.appLocalizations;
    final status = _status;
    final peers = (status?['peers'] as List?) ?? const [];
    final state = switch (status?['state']) {
      'connected' =>
        status?['management'] == true
            ? l.netBirdConnected
            : l.netBirdConnecting,
      'connecting' => l.netBirdConnecting,
      'error' => l.netBirdConnectionError,
      _ => l.netBirdStopped,
    };
    return CommonScaffold(
      title: 'NetBird',
      body: Builder(
        builder: (context) => ListView(
          padding: const EdgeInsets.symmetric(horizontal: 16).copyWith(
            top: context.sectionsTopPadding,
            bottom: 20 + context.bottomInset,
          ),
          children: [
            ErrorBanner(message: _loadFailed ? l.netBirdLoadError : null),
            if (status != null) ...[
              generateSection(
                items: [
                  ListItem(title: Text(l.status), subtitle: Text(state)),
                  if ((status['ip'] as String? ?? '').isNotEmpty)
                    ListItem(
                      title: const Text('IP'),
                      subtitle: Text(status['ip'] as String),
                    ),
                  ListItem(
                    title: Text(l.netBirdConfiguration),
                    subtitle: Text(status['managementUrl'] as String? ?? ''),
                    onTap: _edit,
                    trailing: const GlyphIcon(AppGlyphs.chevronForward),
                  ),
                ],
                footer: l.netBirdDescription,
              ),
              if (status['state'] == 'error')
                generateSection(
                  items: [ListItem(title: Text(l.netBirdConnectionError))],
                  footer: l.netBirdRetryHint,
                ),
              generateSection(
                title: l.netBirdPeers,
                items: [
                  for (final peer in peers.whereType<Map>())
                    ListItem(
                      title: Text(peer['name'] as String? ?? ''),
                      subtitle: Text(
                        '${peer['ip'] ?? ''} · ${peer['connected'] == true ? (peer['relayed'] == true ? l.netBirdRelayed : l.netBirdP2P) : l.netBirdStopped}',
                      ),
                    ),
                  if (peers.isEmpty) ListItem(title: Text(l.netBirdNoPeers)),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _NetBirdForm extends ConsumerStatefulWidget {
  const _NetBirdForm({required this.status});
  final Map<String, dynamic> status;

  @override
  ConsumerState<_NetBirdForm> createState() => _NetBirdFormState();
}

class _NetBirdFormState extends ConsumerState<_NetBirdForm> {
  late final TextEditingController _server;
  late final TextEditingController _name;
  late final TextEditingController _cidr;
  final _key = TextEditingController();
  late bool _enabled;
  bool _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _server = TextEditingController(
      text: widget.status['managementUrl'] as String? ?? '',
    );
    _name = TextEditingController(
      text: widget.status['deviceName'] as String? ?? '',
    );
    _cidr = TextEditingController(text: widget.status['cidr'] as String? ?? '');
    _enabled = widget.status['enabled'] == true;
  }

  Future<void> _save() async {
    if (_saving) return;
    final l = context.appLocalizations;
    if (ref.read(runTimeProvider) != null) {
      setState(() => _error = l.netBirdStopBeforeEditing);
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final accepted = await ref.read(coreHandlerProvider).configureNetBird({
        'enabled': _enabled,
        'managementUrl': _server.text.trim(),
        'deviceName': _name.text.trim(),
        'cidr': _cidr.text.trim(),
        'setupKey': _key.text.trim(),
      });
      if (!accepted) throw StateError('NetBird settings rejected');
      final applied = await ref
          .read(setupActionProvider.notifier)
          .applyProfile(force: true);
      if (!applied) throw StateError('NetBird profile apply failed');
      _key.clear();
      if (mounted) Navigator.of(context).pop();
    } catch (_) {
      if (mounted) setState(() => _error = l.netBirdSaveError);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  void dispose() {
    _server.dispose();
    _name.dispose();
    _cidr.dispose();
    _key.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final l = context.appLocalizations;
    return FormPage(
      title: l.netBirdConfiguration,
      saving: _saving,
      error: _error,
      onSave: _save,
      children: [
        generateSection(
          items: [
            ListItem(
              title: Text(l.netBirdEnabled),
              trailing: CommonSwitch(
                value: _enabled,
                onChanged: _saving
                    ? null
                    : (value) => setState(() => _enabled = value),
              ),
            ),
            FormRow(
              title: l.netBirdServer,
              trailing: FormRowTextField(
                controller: _server,
                autocorrect: false,
              ),
            ),
            FormRow(
              title: l.netBirdDeviceName,
              trailing: FormRowTextField(controller: _name, autocorrect: false),
            ),
            FormRow(
              title: l.netBirdNetwork,
              trailing: FormRowTextField(controller: _cidr, autocorrect: false),
            ),
            if (widget.status['enrolled'] != true)
              FormRow(
                title: l.netBirdSetupKey,
                trailing: FormRowSecretTextField(controller: _key),
              ),
          ],
          footer: l.netBirdConfigurationHint,
        ),
      ],
    );
  }
}
