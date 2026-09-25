import 'dart:convert';

import 'package:flutter/material.dart';

import '../chat/tool_stats.dart';

/// One tool call: a one-line summary plus the raw Input/Output on expand
/// (mirrors `web/src/components/ToolCallCard.tsx`).
class ToolCard extends StatelessWidget {
  const ToolCard({super.key, required this.call});

  final ToolCall call;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final input = formatValue(call.input);
    final output = formatValue(call.output);
    final running = call.status == 'pending' || call.status == 'in_progress';

    return ExpansionTile(
      dense: true,
      tilePadding: const EdgeInsets.symmetric(horizontal: 12),
      leading: running
          ? const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : Icon(
              call.status == 'failed' ? Icons.error_outline : Icons.check_circle_outline,
              size: 18,
              color: call.status == 'failed'
                  ? theme.colorScheme.error
                  : theme.colorScheme.primary,
            ),
      title: Text(summaryOf(call), style: theme.textTheme.bodySmall),
      childrenPadding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
      expandedCrossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (input.isNotEmpty) _Section(title: 'Input', body: input),
        if (output.isNotEmpty) _Section(title: 'Output', body: output),
      ],
    );
  }
}

class _Section extends StatelessWidget {
  const _Section({required this.title, required this.body});

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: theme.textTheme.labelMedium),
        const SizedBox(height: 4),
        Container(
          width: double.infinity,
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            color: theme.colorScheme.surfaceContainerHighest,
            borderRadius: BorderRadius.circular(8),
          ),
          child: SelectableText(
            body,
            style: theme.textTheme.bodySmall?.copyWith(fontFamily: 'monospace'),
          ),
        ),
        const SizedBox(height: 8),
      ],
    );
  }
}

/// `Title · key=value` — the first usable input field makes the row readable.
String summaryOf(ToolCall call) {
  final title = call.title.isNotEmpty ? call.title : '工具调用';
  final input = call.input;
  if (input is Map) {
    for (final entry in input.entries) {
      final value = entry.value;
      if (value is String && value.isNotEmpty && value.length <= 80) {
        return '$title · ${entry.key}=$value';
      }
    }
  }
  return title;
}

/// Pretty JSON for objects; raw text for strings.
String formatValue(Object? value) {
  if (value == null) return '';
  if (value is String) return value;
  try {
    return const JsonEncoder.withIndent('  ').convert(value);
  } catch (_) {
    return '$value';
  }
}
