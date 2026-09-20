import 'package:flutter/material.dart';

import '../chat/chat_items.dart';
import 'markdown_view.dart';
import 'tool_card.dart';

/// One turn's thinking + tools, collapsed into a single card with a summary
/// line (e.g. "Edited 3 files, ran 2 commands") — the mobile equivalent of the
/// web console's activity group.
class ActivityBlock extends StatelessWidget {
  const ActivityBlock({super.key, required this.item});

  final ActivityItem item;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final stats = item.stats;
    final running = item.streaming;

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: Theme(
        data: theme.copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          tilePadding: const EdgeInsets.symmetric(horizontal: 12),
          childrenPadding: const EdgeInsets.only(bottom: 8),
          leading: running
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : Icon(Icons.bolt_outlined, size: 18, color: theme.colorScheme.primary),
          title: Text(
            stats.label.isEmpty ? '工作过程' : stats.label,
            style: theme.textTheme.bodySmall,
          ),
          subtitle: stats.plus > 0 || stats.minus > 0
              ? Text(
                  '+${stats.plus} / -${stats.minus}',
                  style: theme.textTheme.labelSmall,
                )
              : null,
          children: [
            if (item.hasThought)
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 0, 12, 8),
                child: Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.surfaceContainerHighest,
                    borderRadius: BorderRadius.circular(8),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('思考', style: theme.textTheme.labelMedium),
                      const SizedBox(height: 4),
                      MarkdownView(text: item.thoughtText),
                    ],
                  ),
                ),
              ),
            for (final entry in item.entries)
              if (entry is ToolActivity) ToolCard(call: entry.call),
          ],
        ),
      ),
    );
  }
}
