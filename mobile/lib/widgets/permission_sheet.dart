import 'package:flutter/material.dart';

import '../ws/frames.dart';

/// Asks the user to answer an ACP permission request. Returns the chosen
/// option, or null when dismissed.
Future<PermissionOption?> showPermissionSheet(
  BuildContext context,
  PermissionRequest request,
) {
  return showModalBottomSheet<PermissionOption>(
    context: context,
    isScrollControlled: true,
    builder: (context) {
      final theme = Theme.of(context);
      return SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('需要你的许可', style: theme.textTheme.titleMedium),
              const SizedBox(height: 8),
              Text(
                request.title.isEmpty ? '助手请求执行一个操作' : request.title,
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 16),
              for (final option in request.options)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: SizedBox(
                    width: double.infinity,
                    child: _isReject(option)
                        ? OutlinedButton(
                            onPressed: () => Navigator.of(context).pop(option),
                            child: Text(_label(option)),
                          )
                        : FilledButton(
                            onPressed: () => Navigator.of(context).pop(option),
                            child: Text(_label(option)),
                          ),
                  ),
                ),
            ],
          ),
        ),
      );
    },
  );
}

/// Mirrors the web console's mapping for the assist ticket resolution.
String resolutionFor(String optionId) =>
    optionId.contains('reject') ? 'reject' : 'allow_once';

bool _isReject(PermissionOption option) =>
    option.kind == 'reject_once' ||
    option.kind == 'reject_always' ||
    resolutionFor(option.optionId) == 'reject';

String _label(PermissionOption option) =>
    option.name.isNotEmpty ? option.name : option.optionId;
