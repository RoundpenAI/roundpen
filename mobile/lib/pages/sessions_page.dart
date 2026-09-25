import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../api/client.dart';
import '../api/types.dart';
import '../session/session_controller.dart';
import '../widgets/async_list.dart';

class SessionsPage extends StatefulWidget {
  const SessionsPage({super.key, required this.session, required this.assistantId});

  final SessionController session;
  final String assistantId;

  @override
  State<SessionsPage> createState() => _SessionsPageState();
}

class _SessionsPageState extends State<SessionsPage> {
  late Future<List<AgentSession>> _future = _load();

  /// The server returns the user's newest 50 sessions across all assistants and
  /// has no per-assistant filter, so filtering happens here. Known limitation:
  /// an assistant whose sessions all fell outside the newest 50 shows none.
  Future<List<AgentSession>> _load() async {
    final all = await widget.session.endpoints.sessions();
    return all.where((s) => s.assistantId == widget.assistantId).toList();
  }

  Future<void> _refresh() async {
    final next = _load();
    setState(() => _future = next);
    await next;
  }

  Future<void> _create() async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final created = await widget.session.endpoints
          .createSession(assistantId: widget.assistantId);
      if (!mounted) return;
      context.go('/assistants/${widget.assistantId}/sessions/${created.id}');
    } on ApiError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  Future<void> _rename(AgentSession session) async {
    final messenger = ScaffoldMessenger.of(context);
    final controller = TextEditingController(text: session.title);
    final title = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('重命名会话'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(labelText: '标题'),
          onSubmitted: (value) => Navigator.of(context).pop(value),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('取消')),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(controller.text),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (title == null) return;
    if (title.trim().isEmpty) {
      messenger.showSnackBar(const SnackBar(content: Text('标题不能为空')));
      return;
    }
    try {
      await widget.session.endpoints.renameSession(session.id, title.trim());
      if (!mounted) return;
      await _refresh();
    } on ApiError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  Future<void> _delete(AgentSession session) async {
    final messenger = ScaffoldMessenger.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除会话'),
        content: Text('「${session.title}」及其聊天记录将被删除，无法恢复。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await widget.session.endpoints.deleteSession(session.id);
      if (!mounted) return;
      await _refresh();
    } on ApiError catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('会话')),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _create,
        icon: const Icon(Icons.add),
        label: const Text('新建会话'),
      ),
      body: AsyncList<AgentSession>(
        future: _future,
        onRefresh: _refresh,
        emptyText: '这个助手还没有会话，点右下角新建',
        itemBuilder: (context, session) => ListTile(
          title: Text(session.title.isEmpty ? '未命名会话' : session.title),
          onTap: () => context.go('/assistants/${widget.assistantId}/sessions/${session.id}'),
          trailing: PopupMenuButton<String>(
            onSelected: (value) {
              switch (value) {
                case 'rename':
                  _rename(session);
                case 'delete':
                  _delete(session);
              }
            },
            itemBuilder: (context) => const [
              PopupMenuItem(value: 'rename', child: Text('重命名')),
              PopupMenuItem(value: 'delete', child: Text('删除')),
            ],
          ),
        ),
      ),
    );
  }
}
