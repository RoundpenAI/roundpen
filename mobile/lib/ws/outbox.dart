/// In-memory queue for prompts typed while the session socket is not open.
/// Ported from `web/src/lib/sessionOutbox.ts`.
List<String> enqueueOutbox(List<String> queue, String text) {
  final trimmed = text.trim();
  if (trimmed.isEmpty) return queue;
  return [...queue, trimmed];
}

class OutboxDrain {
  const OutboxDrain(this.remaining, this.items);

  final List<String> remaining;
  final List<String> items;
}

OutboxDrain drainOutbox(List<String> queue) =>
    queue.isEmpty ? OutboxDrain(queue, const []) : OutboxDrain(const [], [...queue]);

String? outboxWaitingHint(int queueLength) => queueLength > 0 ? '连接后发送…' : null;
