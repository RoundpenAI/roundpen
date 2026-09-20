import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/client.dart';
import '../api/types.dart';
import '../session/session_controller.dart';
import '../widgets/async_list.dart';

class AssistantsPage extends StatefulWidget {
  const AssistantsPage({super.key, required this.session});

  final SessionController session;

  @override
  State<AssistantsPage> createState() => _AssistantsPageState();
}

class _AssistantsPageState extends State<AssistantsPage> {
  late Future<List<Assistant>> _future = widget.session.endpoints.assistants();

  Future<void> _refresh() async {
    final next = widget.session.endpoints.assistants();
    setState(() => _future = next);
    await next;
  }

  /// Mirrors the web console's "click an assistant → its current chat".
  Future<void> _openCurrentSession(Assistant assistant) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final sessionId = await widget.session.endpoints.ensureSession(assistant.id);
      if (!mounted) return;
      context.go('/assistants/${assistant.id}/sessions/$sessionId');
    } on ApiError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('助手'),
        actions: [
          IconButton(
            tooltip: '设置',
            onPressed: () => context.go('/settings'),
            icon: const Icon(Icons.settings_outlined),
          ),
        ],
      ),
      body: AsyncList<Assistant>(
        future: _future,
        onRefresh: _refresh,
        emptyText: '还没有助手',
        itemBuilder: (context, assistant) => ListTile(
          title: Text(assistant.name),
          subtitle: assistant.bio.isEmpty ? null : Text(assistant.bio, maxLines: 2),
          onTap: () => context.go('/assistants/${assistant.id}'),
          trailing: IconButton(
            tooltip: '打开当前会话',
            icon: const Icon(Icons.forum_outlined),
            onPressed: () => _openCurrentSession(assistant),
          ),
        ),
      ),
    );
  }
}
