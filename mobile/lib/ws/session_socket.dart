import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import '../api/client.dart';
import 'backoff.dart';
import 'frames.dart';
import 'outbox.dart';

enum WsStatus { idle, connecting, open, reconnecting }

/// Transport seam so tests can drive a fake stream instead of the network.
abstract class SessionTransport {
  Stream<List<int>> get events;
  Future<void> postAction(Map<String, Object?> frame);
  Future<void> close();
}

typedef TransportFactory = Future<SessionTransport> Function(
  Uri eventsUri,
  Uri actionsUri,
  Map<String, String> headers,
);

/// Real SSE + POST transport used by the app.
Future<SessionTransport> defaultHttpTransport(
  Uri eventsUri,
  Uri actionsUri,
  Map<String, String> headers,
) async {
  final client = http.Client();
  final req = http.Request('GET', eventsUri)..headers.addAll(headers);
  final streamed = await client.send(req);
  if (streamed.statusCode >= 400) {
    client.close();
    throw ApiError(streamed.statusCode, 'events HTTP ${streamed.statusCode}');
  }
  return _HttpTransport(client, streamed, actionsUri, headers);
}

class _HttpTransport implements SessionTransport {
  _HttpTransport(this._client, this._streamed, this._actionsUri, this._headers);

  final http.Client _client;
  final http.StreamedResponse _streamed;
  final Uri _actionsUri;
  final Map<String, String> _headers;

  @override
  Stream<List<int>> get events => _streamed.stream;

  @override
  Future<void> postAction(Map<String, Object?> frame) async {
    final c = http.Client();
    try {
      final req = http.Request('POST', _actionsUri);
      req.headers['Authorization'] = _headers['Authorization'] ?? '';
      req.headers['Content-Type'] = 'application/json';
      req.headers['Accept'] = 'application/json';
      req.body = jsonEncode(frame);
      final streamed = await c.send(req).timeout(const Duration(seconds: 20));
      final res = await http.Response.fromStream(streamed);
      if (res.statusCode >= 400) {
        throw ApiError(res.statusCode, 'action HTTP ${res.statusCode}');
      }
    } finally {
      c.close();
    }
  }

  @override
  Future<void> close() async {
    _client.close();
  }
}

/// One session's event stream: SSE for server→client, POST /actions for
/// client→server. Reconnects with the web console's backoff schedule and
/// queues prompts typed while it is down.
class SessionSocket {
  SessionSocket({
    required this.api,
    required this.sessionId,
    required this.autoEnabled,
    TransportFactory? transport,
    Future<void> Function(Duration)? delay,
    this.connectTimeout = const Duration(seconds: 10),
  })  : _transportFactory = transport ?? defaultHttpTransport,
        _delay = delay ?? Future<void>.delayed;

  final ApiClient api;
  final String sessionId;

  /// Read at (re)connect time: auto mode is re-announced on every connection.
  final bool Function() autoEnabled;
  final TransportFactory _transportFactory;
  final Future<void> Function(Duration) _delay;

  /// A dial that neither completes nor fails must not wedge the reconnect
  /// loop; this bounds every attempt.
  final Duration connectTimeout;

  void Function(ServerFrame frame)? onFrame;
  void Function(WsStatus status)? onStatus;
  void Function()? onConnected;

  SessionTransport? _conn;
  StreamSubscription<List<int>>? _sub;
  WsStatus _status = WsStatus.idle;
  int _attempt = 0;
  bool _disposed = false;
  bool _dialing = false;
  List<String> _outbox = [];
  final _buf = StringBuffer();

  WsStatus get status => _status;
  int get queued => _outbox.length;

  Uri eventsUri() {
    final base = Uri.parse(api.baseUrl);
    return base.replace(path: '/v1/agent-sessions/$sessionId/events');
  }

  Uri actionsUri() {
    final base = Uri.parse(api.baseUrl);
    return base.replace(path: '/v1/agent-sessions/$sessionId/actions');
  }

  Map<String, String> headers() => {
        'Authorization': 'Bearer ${api.token}',
        'Accept': 'text/event-stream',
      };

  Future<void> connect() async {
    if (_disposed || _dialing || _status == WsStatus.open) return;
    _dialing = true;
    _setStatus(WsStatus.connecting);
    try {
      final conn = await _transportFactory(eventsUri(), actionsUri(), headers())
          .timeout(connectTimeout);
      if (_disposed) {
        await conn.close();
        return;
      }
      _conn = conn;
      _attempt = 0;
      _setStatus(WsStatus.open);
      onConnected?.call();
      send(autoFrame(autoEnabled()));
      _buf.clear();
      _sub = conn.events.listen(
        _onBytes,
        onDone: _onClosed,
        onError: (Object _) => _onClosed(),
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect();
    } finally {
      _dialing = false;
    }
  }

  /// Sends a frame via POST /actions; returns false when the stream is not open.
  bool send(Map<String, Object?> frame) {
    final conn = _conn;
    if (_status != WsStatus.open || conn == null) return false;
    unawaited(conn.postAction(frame).catchError((Object e) {
      onFrame?.call(ServerFrame.parse(jsonEncode({
        'type': 'error',
        'message': e is ApiError ? e.message : 'action failed',
      })));
    }));
    return true;
  }

  bool prompt(String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return true;
    if (send(promptFrame(trimmed))) return true;
    _outbox = enqueueOutbox(_outbox, trimmed);
    return false;
  }

  void clearOutbox() => _outbox = [];

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

  void _onBytes(List<int> bytes) {
    _buf.write(utf8.decode(bytes, allowMalformed: true));
    var data = _buf.toString();
    final parts = data.split('\n\n');
    _buf
      ..clear()
      ..write(parts.removeLast());
    for (final block in parts) {
      final dataLines = <String>[];
      for (final line in block.split('\n')) {
        if (line.startsWith('data:')) {
          dataLines.add(line.substring(5).trimLeft());
        }
      }
      if (dataLines.isEmpty) continue;
      onFrame?.call(ServerFrame.parse(dataLines.join('\n')));
    }
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
