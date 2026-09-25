import 'package:flutter/material.dart';

import '../ws/session_socket.dart';

/// Status strip: hidden while everything is fine, loud while reconnecting.
class ConnectionChip extends StatelessWidget {
  const ConnectionChip({super.key, required this.status, this.queued = 0});

  final WsStatus status;
  final int queued;

  @override
  Widget build(BuildContext context) {
    if (status == WsStatus.open && queued == 0) return const SizedBox.shrink();
    final theme = Theme.of(context);
    final text = switch (status) {
      WsStatus.idle => '未连接',
      WsStatus.connecting => '连接中…',
      WsStatus.open => '$queued 条待发送',
      WsStatus.reconnecting => '连接断开，重连中…',
    };
    final warn = status == WsStatus.reconnecting || status == WsStatus.idle;
    return Container(
      width: double.infinity,
      color: warn ? theme.colorScheme.errorContainer : theme.colorScheme.surfaceContainerHighest,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Text(text, style: theme.textTheme.labelSmall),
    );
  }
}
