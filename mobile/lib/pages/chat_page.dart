import 'package:flutter/material.dart';

import '../api/client.dart';
import '../api/types.dart';
import '../chat/chat_items.dart';
import '../chat/chat_reducer.dart';
import '../session/session_controller.dart';
import '../widgets/activity_block.dart';
import '../widgets/composer.dart';
import '../widgets/connection_chip.dart';
import '../widgets/message_bubble.dart';
import '../widgets/permission_sheet.dart';
import '../ws/frames.dart';
import '../ws/outbox.dart';
import '../ws/session_socket.dart';

class ChatPage extends StatefulWidget {
  const ChatPage({super.key, required this.session, required this.sessionId});

  final SessionController session;
  final String sessionId;

  @override
  State<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends State<ChatPage> with WidgetsBindingObserver {
  late final ChatReducer _reducer = ChatReducer(onTurnSettled: _refetchHistory);
  late final SessionSocket _socket = SessionSocket(
    api: widget.session.api,
    sessionId: widget.sessionId,
    autoEnabled: () => _auto,
  );
  late final Future<AgentSession> _sessionFuture =
      widget.session.endpoints.getSession(widget.sessionId);
  final ScrollController _scroll = ScrollController();

  bool _auto = true;
  String? _shownPermissionId;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _reducer.addListener(_onStateChanged);
    _socket
      ..onFrame = _onFrame
      ..onConnected = _refetchHistory
      ..onStatus = (_) => setState(() {});
    _boot();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _reducer.removeListener(_onStateChanged);
    _reducer.dispose();
    _socket.dispose();
    _scroll.dispose();
    super.dispose();
  }

  /// Web parity: load the transcript first, then open the socket.
  Future<void> _boot() async {
    await _refetchHistory();
    await _socket.connect();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _socket.resume();
  }

  void _onStateChanged() {
    setState(() {});
    _scrollToEnd();
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scroll.hasClients) return;
      _scroll.jumpTo(_scroll.position.maxScrollExtent);
    });
  }

  Future<void> _refetchHistory() async {
    try {
      final rows = await widget.session.endpoints.messages(widget.sessionId);
      _reducer.applyHistory(rows);
    } on ApiError catch (e) {
      _reducer.setError(e.message);
    }
  }

  void _onFrame(ServerFrame frame) {
    _reducer.apply(frame);
    switch (frame) {
      case StatusFrame(:final busy):
        // Queued prompts go out only when the session is idle.
        if (!busy) _socket.flushOutbox();
      case DoneFrame():
        _socket.flushOutbox();
      case PermissionRequestFrame(:final request):
        _showPermission(request);
      case PermissionResolvedFrame(:final requestId):
        _closePermissionSheet(requestId);
      case ClearedFrame():
        // The context was wiped server-side; queued prompts belong to it.
        _socket.clearOutbox();
        _reducer.setNotice(null);
      default:
        break;
    }
  }

  Future<void> _showPermission(PermissionRequest request) async {
    if (_shownPermissionId != null) return;
    _shownPermissionId = request.requestId;
    final option = await showPermissionSheet(context, request);
    if (_shownPermissionId != request.requestId) return; // resolved elsewhere
    _shownPermissionId = null;
    if (option == null || !mounted) return;

    _socket.send(permissionFrame(request.requestId, option.optionId));
    if (request.ticketId.isNotEmpty) {
      try {
        await widget.session.endpoints.resolveTicket(
          request.ticketId,
          resolutionFor(option.optionId),
          option.optionId,
        );
      } on ApiError {
        // Best effort — the WS answer is what actually unblocks the turn.
      }
    }
  }

  void _closePermissionSheet(String requestId) {
    if (_shownPermissionId != requestId) return;
    _shownPermissionId = null;
    final navigator = Navigator.of(context);
    if (navigator.canPop()) navigator.pop();
  }

  void _send(String text) {
    _reducer.appendLocalUser(text);
    _reducer.setBusy(true);
    _reducer.setError(null);
    if (!_socket.prompt(text)) {
      _reducer.setNotice(outboxWaitingHint(_socket.queued));
    }
  }

  void _toggleAuto(bool value) {
    setState(() => _auto = value);
    _socket.send(autoFrame(value));
  }

  List<ChatItem> _visibleItems(ChatState state) => [
        ...state.items,
        if (state.liveEntries.isNotEmpty)
          ActivityItem(entries: state.liveEntries, streaming: state.busy),
        if (state.streamingReply.isNotEmpty)
          AssistantItem(text: state.streamingReply, streaming: state.busy),
      ];

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = _reducer.state;
    final items = _visibleItems(state);

    return Scaffold(
      appBar: AppBar(
        title: FutureBuilder<AgentSession>(
          future: _sessionFuture,
          builder: (context, snapshot) => Text(
            snapshot.data?.title ?? '会话',
            overflow: TextOverflow.ellipsis,
          ),
        ),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 8),
            child: Row(
              children: [
                Text('Auto', style: theme.textTheme.labelMedium),
                Switch(value: _auto, onChanged: _toggleAuto),
              ],
            ),
          ),
        ],
      ),
      body: Column(
        children: [
          ConnectionChip(status: _socket.status, queued: _socket.queued),
          if (state.error != null)
            MaterialBanner(
              content: Text(state.error!),
              actions: [
                TextButton(
                  onPressed: () => _reducer.setError(null),
                  child: const Text('知道了'),
                ),
              ],
            ),
          Expanded(
            child: items.isEmpty
                ? const Center(child: Text('开始和助手对话吧'))
                : ListView.builder(
                    controller: _scroll,
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    itemCount: items.length,
                    itemBuilder: (context, i) => _buildItem(items[i]),
                  ),
          ),
          Composer(
            busy: state.busy,
            onSend: _send,
            onStop: () => _socket.send(cancelFrame()),
            hint: state.notice,
          ),
        ],
      ),
    );
  }

  Widget _buildItem(ChatItem item) {
    switch (item) {
      case UserItem(:final text, :final display):
        return MessageBubble(text: display ?? text, fromUser: true);
      case AssistantItem(:final text, :final streaming):
        return MessageBubble(text: text, fromUser: false, streaming: streaming);
      case ActivityItem():
        return ActivityBlock(item: item);
      case DividerItem(:final label):
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
          child: Row(
            children: [
              const Expanded(child: Divider()),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8),
                child: Text(label, style: Theme.of(context).textTheme.labelSmall),
              ),
              const Expanded(child: Divider()),
            ],
          ),
        );
      case SystemItem(:final text):
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
          child: Text(
            text,
            style: Theme.of(context).textTheme.labelSmall,
            textAlign: TextAlign.center,
          ),
        );
    }
  }
}
