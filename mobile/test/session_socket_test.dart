import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/api/client.dart';
import 'package:roundpen_console/ws/backoff.dart';
import 'package:roundpen_console/ws/frames.dart';
import 'package:roundpen_console/ws/outbox.dart';
import 'package:roundpen_console/ws/session_socket.dart';

class FakeTransport implements SessionTransport {
  final StreamController<List<int>> _controller = StreamController<List<int>>();
  final List<Map<String, Object?>> posted = [];
  bool closed = false;

  @override
  Stream<List<int>> get events => _controller.stream;

  @override
  Future<void> postAction(Map<String, Object?> frame) async {
    posted.add(frame);
  }

  @override
  Future<void> close() async {
    closed = true;
    if (!_controller.isClosed) await _controller.close();
  }

  void emitFrame(Map<String, Object?> frame) {
    final raw = 'data: ${jsonEncode(frame)}\n\n';
    _controller.add(utf8.encode(raw));
  }

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
    test('authenticates the stream and announces auto mode', () async {
      final api = testApi();
      late Uri seenEvents;
      late Map<String, String> seenHeaders;
      final fake = FakeTransport();
      final socket = SessionSocket(
        api: api,
        sessionId: 's1',
        autoEnabled: () => true,
        transport: (events, actions, headers) async {
          seenEvents = events;
          seenHeaders = headers;
          return fake;
        },
      );

      await socket.connect();
      await pump();
      expect(seenEvents.path, '/v1/agent-sessions/s1/events');
      expect(seenHeaders['Authorization'], 'Bearer tok');
      expect(fake.posted, [
        {'type': 'auto', 'enabled': true},
      ]);
      socket.dispose();
    });

    test('parses SSE data frames', () async {
      final fake = FakeTransport();
      final frames = <ServerFrame>[];
      final socket = SessionSocket(
        api: testApi(),
        sessionId: 's1',
        autoEnabled: () => false,
        transport: (_, __, ___) async => fake,
      );
      socket.onFrame = frames.add;
      await socket.connect();
      fake.emitFrame({'type': 'hello', 'message': 'ok'});
      await pump();
      expect(frames.first.type, 'hello');
      socket.dispose();
    });

    test('queues prompts while reconnecting', () async {
      final delay = ControlledDelay();
      final fake = FakeTransport();
      var calls = 0;
      final socket = SessionSocket(
        api: testApi(),
        sessionId: 's1',
        autoEnabled: () => false,
        delay: delay,
        transport: (_, __, ___) async {
          calls += 1;
          if (calls == 1) {
            return fake;
          }
          return Completer<SessionTransport>().future;
        },
      );
      await socket.connect();
      expect(socket.prompt('hi'), isTrue);
      fake.dropConnection();
      await pump();
      expect(socket.prompt('queued'), isFalse);
      expect(socket.queued, 1);
      socket.dispose();
    });
  });

  group('frames', () {
    test('promptFrame shape', () {
      expect(promptFrame('x'), {'type': 'prompt', 'text': 'x'});
    });
  });
}
