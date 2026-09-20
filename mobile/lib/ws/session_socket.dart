import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../api/client.dart';
import 'backoff.dart';
import 'frames.dart';
import 'outbox.dart';

/// Transport seam so tests can drive a fake socket instead of the network.
abstract class WsConnection {
  Stream<dynamic> get stream;
  void send(String text);
  Future<void> close();
}

typedef WsConnector = Future<WsConnection> Function(Uri uri, Map<String, String> headers);

/// Native WebSocket with handshake headers — the reason the server needs no
/// ticket mechanism (browsers cannot do this, RN/iOS cannot either).
Future<WsConnection> defaultWsConnector(Uri uri, Map<String, String> headers) async {
  final ws = await WebSocket.connect(uri.toString(), headers: headers);
  return _IoWsConnection(ws);
}

class _IoWsConnection implements WsConnection {
  _IoWsConnection(this._ws);

  final WebSocket _ws;

  @override
  Stream<dynamic> get stream => _ws;

  @override
  void send(String text) => _ws.add(text);

  @override
  Future<void> close() => _ws.close();
}

enum WsStatus { idle, connecting, open, reconnecting }

/// One session's WebSocket: authenticates the upgrade with the session token,
/// reconnects with the web console's backoff schedule, and queues prompts typed
/// while it is down.
class SessionSocket {
  SessionSocket({
    required this.api,
    required this.sessionId,
    required this.autoEnabled,
    WsConnector? connector,
    Future<void> Function(Duration)? delay,
    this.connectTimeout = const Duration(seconds: 10),
  })  : _connector = connector ?? defaultWsConnector,
        _delay = delay ?? Future<void>.delayed;

  final ApiClient api;
  final String sessionId;

  /// Read at (re)connect time: auto mode is re-announced on every connection.
  final bool Function() autoEnabled;
  final WsConnector _connector;
  final Future<void> Function(Duration) _delay;

  /// A dial that neither completes nor fails (a black-holed TCP connect) must
  /// not wedge the reconnect loop; this bounds every attempt.
  final Duration connectTimeout;

  void Function(ServerFrame frame)? onFrame;
  void Function(WsStatus status)? onStatus;
  void Function()? onConnected;

  WsConnection? _conn;
  StreamSubscription<dynamic>? _sub;
  WsStatus _status = WsStatus.idle;
  int _attempt = 0;
  bool _disposed = false;
  bool _dialing = false;
  List<String> _outbox = [];

  WsStatus get status => _status;
  int get queued => _outbox.length;

  /// Control-plane paths hang off the origin root; the address the user typed
  /// is an origin, so only scheme/host/port carry over.
  Uri wsUri() {
    final base = Uri.parse(api.baseUrl);
    return Uri(
      scheme: base.scheme == 'https' ? 'wss' : 'ws',
      host: base.host,
      port: base.hasPort ? base.port : null,
      path: '/v1/agent-sessions/$sessionId/ws',
    );
  }

  Map<String, String> headers() => {'Authorization': 'Bearer ${api.token}'};

  Future<void> connect() async {
    if (_disposed || _dialing || _status == WsStatus.open) return;
    _dialing = true;
    _setStatus(WsStatus.connecting);
    try {
      final conn = await _connector(wsUri(), headers()).timeout(connectTimeout);
      if (_disposed) {
        await conn.close();
        return;
      }
      _conn = conn;
      _attempt = 0;
      _setStatus(WsStatus.open);
      onConnected?.call();
      send(autoFrame(autoEnabled()));
      _sub = conn.stream.listen(
        _onData,
        onDone: _onClosed,
        onError: (Object _) => _onClosed(),
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect();
    } finally {
      // Must always clear: a stuck flag here used to kill the retry loop for
      // good (every later attempt returned early).
      _dialing = false;
    }
  }

  /// Sends a frame; returns false when the socket is not open.
  bool send(Map<String, Object?> frame) {
    final conn = _conn;
    if (_status != WsStatus.open || conn == null) return false;
    conn.send(jsonEncode(frame));
    return true;
  }

  /// Sends a prompt now, or queues it until the socket is back.
  /// Returns true when it went out immediately.
  bool prompt(String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return true;
    if (send(promptFrame(trimmed))) return true;
    _outbox = enqueueOutbox(_outbox, trimmed);
    return false;
  }

  /// Drops queued prompts (a `/clear` wipes them with the context).
  void clearOutbox() => _outbox = [];

  /// Flushes queued prompts, in order. Callers run this when the session is
  /// idle (`busy:false` / `done`) so nothing lands mid-turn.
  void flushOutbox() {
    if (_outbox.isEmpty) return;
    final drain = drainOutbox(_outbox);
    _outbox = drain.remaining;
    for (final item in drain.items) {
      if (!send(promptFrame(item))) {
        _outbox = enqueueOutbox(_outbox, item);
      }
    }
  }

  /// Back to the foreground: retry immediately instead of waiting out the
  /// backoff.
  void resume() {
    if (_disposed) return;
    _attempt = 0;
    if (_status != WsStatus.open && _status != WsStatus.connecting) {
      unawaited(connect());
    }
  }

  void dispose() {
    _disposed = true;
    _sub?.cancel();
    _sub = null;
    final conn = _conn;
    _conn = null;
    _status = WsStatus.idle;
    if (conn != null) unawaited(conn.close());
  }

  void _onData(dynamic data) {
    if (data is String) onFrame?.call(ServerFrame.parse(data));
  }

  void _onClosed() {
    _sub?.cancel();
    _sub = null;
    _conn = null;
    if (_disposed) return;
    _scheduleReconnect();
  }

  void _scheduleReconnect() {
    if (_disposed) return;
    _setStatus(WsStatus.reconnecting);
    _attempt += 1;
    final wait = Duration(milliseconds: wsReconnectDelayMs(_attempt));
    unawaited(_delay(wait).then((_) => connect()));
  }

  void _setStatus(WsStatus status) {
    _status = status;
    onStatus?.call(status);
  }
}
