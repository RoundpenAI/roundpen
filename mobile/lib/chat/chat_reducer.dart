import 'package:flutter/foundation.dart';

import '../api/types.dart';
import '../ws/frames.dart';
import 'chat_items.dart';
import 'tool_stats.dart';

/// Everything the chat screen renders, folded from history rows and live WS
/// frames. Pure logic: no widgets, no network.
class ChatState {
  const ChatState({
    this.items = const [],
    this.liveEntries = const [],
    this.streamingReply = '',
    this.busy = false,
    this.permission,
    this.error,
    this.notice,
  });

  /// Settled transcript (from `GET /messages`).
  final List<ChatItem> items;

  /// The in-flight turn: thoughts + tools seen live (not yet persisted).
  final List<ActivityEntry> liveEntries;

  /// In-flight assistant text. `status.reply` REPLACES this on reconnect.
  final String streamingReply;

  final bool busy;
  final PermissionRequest? permission;
  final String? error;

  /// Transient hint, e.g. "连接后发送…" for queued prompts.
  final String? notice;

  ChatState copyWith({
    List<ChatItem>? items,
    List<ActivityEntry>? liveEntries,
    String? streamingReply,
    bool? busy,
    PermissionRequest? permission,
    bool clearPermission = false,
    String? error,
    bool clearError = false,
    String? notice,
    bool clearNotice = false,
  }) {
    return ChatState(
      items: items ?? this.items,
      liveEntries: liveEntries ?? this.liveEntries,
      streamingReply: streamingReply ?? this.streamingReply,
      busy: busy ?? this.busy,
      permission: clearPermission ? null : (permission ?? this.permission),
      error: clearError ? null : (error ?? this.error),
      notice: clearNotice ? null : (notice ?? this.notice),
    );
  }
}

/// Folds frames and history into [ChatState]. Extends ChangeNotifier so the
/// page can rebuild on every change.
class ChatReducer extends ChangeNotifier {
  ChatReducer({this.onTurnSettled});

  /// Called when the server says the turn is over (`done`, or `status` with
  /// busy=false) — the page refetches history then.
  final void Function()? onTurnSettled;

  ChatState _state = const ChatState();
  ChatState get state => _state;

  void _set(ChatState next) {
    _state = next;
    notifyListeners();
  }

  void apply(ServerFrame frame) {
    switch (frame) {
      case HelloFrame():
        break;
      case StatusFrame():
        _applyStatus(frame);
      case EventFrame():
        _applyEvent(frame.event);
      case DoneFrame():
        _set(_state.copyWith(
          busy: false,
          streamingReply: '',
          liveEntries: const [],
        ));
        onTurnSettled?.call();
      case ErrorFrame():
        _set(_state.copyWith(
          busy: false,
          streamingReply: '',
          liveEntries: const [],
          error: frame.message,
        ));
        onTurnSettled?.call();
      case PermissionRequestFrame():
        _set(_state.copyWith(permission: frame.request));
      case PermissionResolvedFrame():
        if (_state.permission?.requestId == frame.requestId) {
          _set(_state.copyWith(clearPermission: true));
        }
      case ClearedFrame():
        _set(_state.copyWith(
          items: [..._state.items, const DividerItem()],
          streamingReply: '',
          liveEntries: const [],
          busy: false,
        ));
      case UnknownFrame():
        final text = frame.type.isEmpty ? '收到无法识别的消息' : frame.type;
        _set(_state.copyWith(items: [..._state.items, SystemItem(text)]));
    }
  }

  void _applyStatus(StatusFrame frame) {
    final entries = [..._state.liveEntries];
    // The snapshot carries the whole thought buffer: replace, never append.
    if (frame.thought.isNotEmpty) {
      final idx = entries.lastIndexWhere((e) => e is ThoughtActivity);
      if (idx >= 0) {
        entries[idx] = ThoughtActivity(frame.thought);
      } else {
        entries.add(ThoughtActivity(frame.thought));
      }
    }
    final wasBusy = _state.busy;
    _set(_state.copyWith(
      busy: frame.busy,
      streamingReply: frame.reply,
      liveEntries: entries,
      permission: frame.perm,
      clearPermission: frame.perm == null,
    ));
    if (wasBusy && !frame.busy) onTurnSettled?.call();
  }

  void _applyEvent(AgentEvent event) {
    switch (event.type) {
      case 'agent_message':
        if (event.text.isEmpty) return;
        _set(_state.copyWith(streamingReply: _state.streamingReply + event.text));
      case 'agent_thought':
        if (event.text.isEmpty) return;
        final entries = [..._state.liveEntries];
        final idx = entries.lastIndexWhere((e) => e is ThoughtActivity);
        if (idx >= 0) {
          final existing = entries[idx] as ThoughtActivity;
          entries[idx] = ThoughtActivity(existing.text + event.text);
        } else {
          entries.add(ThoughtActivity(event.text));
        }
        _set(_state.copyWith(liveEntries: entries));
      case 'tool_call':
      case 'tool_call_update':
        _upsertTool(event);
      case 'plan':
        if (event.text.isEmpty) return;
        _set(_state.copyWith(
          liveEntries: [..._state.liveEntries, ThoughtActivity(event.text)],
        ));
      case 'command_result':
        if (event.text.isEmpty) return;
        _set(_state.copyWith(streamingReply: _state.streamingReply + event.text));
      case 'command_error':
        _set(_state.copyWith(items: [..._state.items, SystemItem(event.text)]));
      default:
        final text = event.text.isNotEmpty ? event.text : event.type;
        _set(_state.copyWith(items: [..._state.items, SystemItem(text)]));
    }
  }

  void _upsertTool(AgentEvent event) {
    final toolId = event.toolId.isNotEmpty ? event.toolId : 'anon-${DateTime.now().microsecondsSinceEpoch}';
    final entries = [..._state.liveEntries];
    final idx = entries.lastIndexWhere((e) => e is ToolActivity && e.toolId == toolId);
    final merged = _mergeTool(
      idx >= 0 ? (entries[idx] as ToolActivity).call : null,
      event,
    );
    final entry = ToolActivity(toolId: toolId, call: merged);
    if (idx >= 0) {
      entries[idx] = entry;
    } else {
      entries.add(entry);
    }
    _set(_state.copyWith(liveEntries: entries));
  }

  /// A `tool_call_update` may carry only some fields; keep what we had.
  ToolCall _mergeTool(ToolCall? previous, AgentEvent event) {
    if (previous == null) {
      return ToolCall(
        title: event.title,
        status: event.status,
        kind: event.kind,
        input: event.input,
        output: event.output,
      );
    }
    return ToolCall(
      title: event.title.isNotEmpty ? event.title : previous.title,
      status: event.status.isNotEmpty ? event.status : previous.status,
      kind: event.kind.isNotEmpty ? event.kind : previous.kind,
      input: event.input ?? previous.input,
      output: event.output ?? previous.output,
    );
  }

  /// Rebuilds the settled transcript from persisted rows, and drops the live
  /// entries the server has now stored (matched by tool id) so nothing doubles.
  void applyHistory(List<MessageRow> rows) {
    final historyToolIds = <String>{
      for (final row in rows)
        if (row.meta['toolId'] is String) row.meta['toolId'] as String,
    };
    final remainingLive = _state.liveEntries
        .where((e) => e is! ToolActivity || !historyToolIds.contains(e.toolId))
        .toList();
    _set(_state.copyWith(items: itemsFromRows(rows), liveEntries: remainingLive));
  }

  /// Optimistic echo of a prompt the user just sent.
  void appendLocalUser(String text) {
    _set(_state.copyWith(items: [..._state.items, UserItem(text: text)]));
  }

  void setBusy(bool value) => _set(_state.copyWith(busy: value));

  void setError(String? message) => _set(message == null
      ? _state.copyWith(clearError: true)
      : _state.copyWith(error: message));

  void setNotice(String? notice) {
    _set(notice == null
        ? _state.copyWith(clearNotice: true)
        : _state.copyWith(notice: notice));
  }

  void clearError() => _set(_state.copyWith(clearError: true));
}

/// Persisted rows → transcript items, grouping consecutive thought/tool rows
/// into one activity block per turn (same rule as the web adapter).
List<ChatItem> itemsFromRows(List<MessageRow> rows) {
  final out = <ChatItem>[];
  var turn = <ActivityEntry>[];

  void flushTurn() {
    if (turn.isEmpty) return;
    out.add(ActivityItem(entries: turn));
    turn = [];
  }

  for (final row in rows) {
    final type = row.meta['type'] as String? ?? row.role;
    final isThought = row.role == 'thought' || type == 'thought' || type == 'reasoning';
    final isTool = row.role == 'tool' || type == 'tool_call' ||
        (row.meta['toolId'] != null && type != 'permission');

    if (isThought || isTool) {
      if (isThought) {
        if (row.content.isNotEmpty) turn.add(ThoughtActivity(row.content));
      } else {
        turn.add(ToolActivity(
          toolId: row.meta['toolId'] as String? ?? row.id,
          call: ToolCall(
            title: row.meta['title'] as String? ?? '',
            status: row.meta['status'] as String? ?? '',
            kind: row.meta['kind'] as String? ?? '',
            input: row.meta['input'],
            output: row.meta['output'],
          ),
        ));
      }
      continue;
    }
    flushTurn();

    switch (row.role) {
      case 'user':
        final display = row.meta['display'] as String?;
        out.add(UserItem(text: row.content, display: display));
      case 'assistant':
        if (row.content.isNotEmpty) out.add(AssistantItem(text: row.content));
      case 'event':
        if (type == 'clear') {
          out.add(const DividerItem());
        } else {
          out.add(AssistantItem(text: row.content));
        }
      default:
        if (row.content.isNotEmpty) out.add(SystemItem(row.content));
    }
  }
  flushTurn();
  return out;
}
