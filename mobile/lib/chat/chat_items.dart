import 'tool_stats.dart';

/// Rendered transcript rows. Mirrors the grouping in
/// `web/src/lib/semiChatAdapter.ts`: consecutive thought + tool rows collapse
/// into a single [ActivityItem] per turn.
sealed class ChatItem {
  const ChatItem();
}

class UserItem extends ChatItem {
  const UserItem({required this.text, this.display});

  /// What the model actually saw (a `/command` is stored expanded).
  final String text;

  /// What the user typed, when different (meta.display).
  final String? display;
}

class AssistantItem extends ChatItem {
  const AssistantItem({required this.text, this.streaming = false});

  final String text;
  final bool streaming;
}

sealed class ActivityEntry {
  const ActivityEntry();
}

class ThoughtActivity extends ActivityEntry {
  const ThoughtActivity(this.text);
  final String text;
}

class ToolActivity extends ActivityEntry {
  const ToolActivity({required this.toolId, required this.call});
  final String toolId;
  final ToolCall call;
}

class ActivityItem extends ChatItem {
  const ActivityItem({required this.entries, this.streaming = false});

  final List<ActivityEntry> entries;
  final bool streaming;

  List<ToolCall> get toolCalls =>
      entries.whereType<ToolActivity>().map((e) => e.call).toList();

  ToolGroupStats get stats => formatGroupStats(toolCalls);

  bool get hasThought => entries.any((e) => e is ThoughtActivity);

  String get thoughtText =>
      entries.whereType<ThoughtActivity>().map((e) => e.text).join('\n\n');
}

/// A plain line: degraded/unknown server output.
class SystemItem extends ChatItem {
  const SystemItem(this.text);
  final String text;
}

/// `/clear` marker (history stays, the model's context restarts here).
class DividerItem extends ChatItem {
  const DividerItem([this.label = '上下文已清空']);
  final String label;
}
