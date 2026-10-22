import 'package:flutter/material.dart';

import '../chat/delivery_need.dart';

/// Compact always-open-PR action bar — port of web `DeliveryActionBar`.
class DeliveryActionBar extends StatelessWidget {
  const DeliveryActionBar({
    super.key,
    required this.need,
    required this.onSend,
    this.disabled = false,
  });

  final DeliveryNeed need;
  final void Function(String text) onSend;
  final bool disabled;

  @override
  Widget build(BuildContext context) {
    if (!need.hasNeed) return const SizedBox.shrink();
    final theme = Theme.of(context);
    final hint = need.openPR
        ? '有未推送的改动 — 默认开 PR，不要直推默认分支'
        : '有未合并的 PR — 确认后 squash 合并';

    return Material(
      color: theme.colorScheme.surfaceContainerLow,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(hint, style: theme.textTheme.labelSmall),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              runSpacing: 6,
              children: [
                if (need.openPR)
                  FilledButton(
                    onPressed: disabled ? null : () => onSend(deliveryPromptOpenPR),
                    child: const Text('提交并开 PR'),
                  ),
                if (need.mergePR)
                  need.openPR
                      ? TextButton(
                          onPressed:
                              disabled ? null : () => onSend(deliveryPromptMergePR),
                          child: const Text('合并 PR'),
                        )
                      : FilledButton(
                          onPressed:
                              disabled ? null : () => onSend(deliveryPromptMergePR),
                          child: const Text('合并 PR'),
                        ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
