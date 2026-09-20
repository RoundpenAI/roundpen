/// Delay before the Nth reconnect after the last successful open.
/// attempt 0 = retry immediately (app came back to the foreground).
/// Same schedule as the web console (`web/src/lib/sessionWsUi.ts`).
int wsReconnectDelayMs(int attempt) {
  if (attempt <= 0) return 0;
  final ms = 1000 << (attempt - 1);
  return ms > 8000 ? 8000 : ms;
}
