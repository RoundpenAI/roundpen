import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/api/client.dart';
import 'package:roundpen_console/ws/backoff.dart';
import 'package:roundpen_console/ws/frames.dart';
import 'package:roundpen_console/ws/outbox.dart';
import 'package:roundpen_console/ws/session_socket.dart';

class FakeWs implements WsConnection {
  final StreamController<dynamic> _controller = StreamController<dynamic>();
  final List<String> sent = [];
  bool closed = false;

  @override
  Stream<dynamic> get stream => _controller.stream;

  @override
  void send(String text) => sent.add(text);

  @override
  Future<void> close() async {
    closed = true;
    if (!_controller.isClosed) await _controller.close();
  }

  void emit(String raw) => _controller.add(raw);
  void dropConnection() => _controller.close();
}

/// Captures the backoff delays and lets the test release them one by one, so a
/// failing socket cannot spin forever.
class ControlledDelay {
  final List<int> delays = [];
  final List<Completer<void>> pending = [];

  Future<void> call(Duration duration) {
    delays.add(duration.inMilliseconds);
    final completer = Completer<void>();
    pending.add(completer);
    return completer.future;
  }

  void release() => pending.removeAt(0).complete();
}

ApiClient testApi({String baseUrl = 'http://box:19001', String token = 'tok'}) =>
    ApiClient(clientFactory: () => throw UnimplementedError())
      ..configure(baseUrl: baseUrl, token: token);

Future<void> pump() => Future<void>.delayed(Duration.zero);

void main() {
  group('backoff', () {
    test('matches the web schedule', () {
      expect(wsReconnectDelayMs(0), 0);
      expect(wsReconnectDelayMs(1), 1000);
      expect(wsReconnectDelayMs(2), 2000);
      expect(wsReconnectDelayMs(3), 4000);
      expect(wsReconnectDelayMs(4), 8000);
      expect(wsReconnectDelayMs(9), 8000);
    });
  });

  group('outbox', () {
    test('queues trimmed prompts and drops empties', () {
      var queue = <String>[];
      queue = enqueueOutbox(queue, '  你好  ');
      queue = enqueueOutbox(queue, '   ');
      expect(queue, ['你好']);
    });

    test('drains everything in order', () {
      final drain = drainOutbox(['一', '二']);
      expect(drain.items, ['一', '二']);
      expect(drain.remaining, isEmpty);
      expect(outboxWaitingHint(2), '连接后发送…');
      expect(outboxWaitingHint(0), isNull);
    });
  });

  group('SessionSocket', () {
    test('authenticates the upgrade and announces auto mode', () async {
      final api = testApi();
      late Uri seenUri;
      late Map<String, String> seenHeaders;
      final fake = FakeWs();
      final socket = SessionSocket(
        api: api,
        sessionId: 's1',
        autoEnabled: () => true,
        connector: (uri, headers) async {
          seenUri = uri;
          seenHeaders = headers;
          return fake;
        },
      );

      await socket.connect();

      expect(seenUri.toString(), 'ws://box:19001/v1/agent-sessions/s1/ws');
      expect(seenHeaders['Authorization'], 'Bearer tok');
      expect(socket.status, WsStatus.open);
      expect(
        jsonDecode(fake.sent.single),
        {'type': 'auto', 'enabled': true},
        reason: 'auto is re-announced on every (re)connect',
      );
      socket.dispose();
    });

    test('forwards parsed frames to the callback', () async {
      final fake = FakeWs();
      final socket = SessionSocket(
        api: testApi(),
        sessionId: 's1',
        autoEnabled: () => false,
        connector: (_, _) async => fake,
      );
      final frames = <ServerFrame>[];
      socket.onFrame = frames.add;

      await socket.connect();
      fake.emit('{"type":"hello","message":"ok"}');
      fake.emit('{"type":"cleared"}');
      await pump();

      expect(frames, hasLength(2));
      expect(frames[0], isA<HelloFrame>());
      expect(frames[1], isA<ClearedFrame>());
      socket.dispose();
    });

    test('queues prompts while closed and flushes them on demand', () async {
      final fake = FakeWs();
      final socket = SessionSocket(
        api: testApi(),
        sessionId: 's1',
        autoEnabled: () => true,
        connector: (_, _) async => fake,
      );

      expect(socket.prompt('第一条'), isFalse);
      expect(socket.prompt('第二条'), isFalse);
      expect(socket.queued, 2);

      await socket.connect();
      // Auto went out on connect; the queued prompts wait for busy:false/done.
      expect(socket.queued, 2);

      socket.flushOutbox();
      final sent = fake.sent.map(jsonDecode).toList();
      expect(sent.map((f) => f['type']), ['auto', 'prompt', 'prompt']);
      expect(sent[1]['text'], '第一条');
      expect(sent[2]['text'], '第二条');
      expect(socket.queued, 0);

      expect(socket.prompt('直接发'), isTrue);
      expect(jsonDecode(fake.sent.last)['text'], '直接发');
      socket.dispose();
    });

    test('reconnects with the same credential after a failure', () async {
      final api = testApi();
      final headers = <String?>[];
      var calls = 0;
      final fake = FakeWs();
      final delay = ControlledDelay();
      final socket = SessionSocket(
        api: api,
        sessionId: 's1',
        autoEnabled: () => true,
        delay: delay.call,
        connector: (_, h) async {
          headers.add(h['Authorization']);
          calls += 1;
          if (calls == 1) throw const SocketExceptionStub();
          return fake;
        },
      );

      await socket.connect();
      await pump();
      expect(socket.status, WsStatus.reconnecting);
      expect(delay.delays, [1000]);

      delay.release();
      await pump();
      expect(socket.status, WsStatus.open);
      expect(headers, ['Bearer tok', 'Bearer tok']);
      expect(socket.queued, 0);
      socket.dispose();
    });

    test('backs off exponentially and caps at 8s', () async {
      final delay = ControlledDelay();
      final socket = SessionSocket(
        api: testApi(),
        sessionId: 's1',
        autoEnabled: () => true,
        delay: delay.call,
        connector: (_, _) async => throw const SocketExceptionStub(),
      );

      await socket.connect();
      await pump();
      for (var i = 0; i < 4; i++) {
        delay.release();
        await pump();
      }

      expect(delay.delays, [1000, 2000, 4000, 8000, 8000]);
      socket.dispose();

      final before = delay.delays.length;
      delay.release();
      await pump();
      expect(delay.delays.length, before, reason: 'dispose stops the loop');
    });

    test('resume retries immediately after the app comes back', () async {
      final delay = ControlledDelay();
      final socket = SessionSocket(
        api: testApi(),
        sessionId: 's1',
        autoEnabled: () => true,
        delay: delay.call,
        connector: (_, _) async => throw const SocketExceptionStub(),
      );

      await socket.connect();
      await pump();
      expect(delay.delays, [1000]);

      socket.resume();
      await pump();
      // resume() resets the attempt counter, so the next failure waits 1s again.
      expect(delay.delays, [1000, 1000]);
      socket.dispose();
    });
  });
}

/// Stand-in for dart:io's SocketException so tests stay platform-free.
class SocketExceptionStub implements Exception {
  const SocketExceptionStub();
}
