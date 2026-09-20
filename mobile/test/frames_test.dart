import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/ws/frames.dart';

void main() {
  test('parses every server frame type', () {
    expect(ServerFrame.parse('{"type":"hello","message":"ok"}'), isA<HelloFrame>());

    final status = ServerFrame.parse(jsonEncode({
      'type': 'status',
      'busy': true,
      'reply': '正在做',
      'thought': '想一下',
      'perm': {
        'requestId': 'r1',
        'title': 'rm -rf',
        'options': [
          {'optionId': 'allow_once', 'name': '允许一次', 'kind': 'allow_once'},
        ],
        'ticketId': 't1',
      },
    }));
    expect(status, isA<StatusFrame>());
    final s = status as StatusFrame;
    expect(s.busy, isTrue);
    expect(s.reply, '正在做');
    expect(s.thought, '想一下');
    expect(s.perm?.requestId, 'r1');
    expect(s.perm?.options.single.optionId, 'allow_once');
    expect(s.perm?.ticketId, 't1');

    final event = ServerFrame.parse(jsonEncode({
      'type': 'event',
      'event': {
        'type': 'tool_call',
        'title': 'Read',
        'status': 'in_progress',
        'kind': 'read',
        'toolId': 'tool-1',
        'input': {'path': 'a.ts'},
        'output': 'x',
      },
    }));
    expect(event, isA<EventFrame>());
    final e = (event as EventFrame).event;
    expect(e.type, 'tool_call');
    expect(e.toolId, 'tool-1');
    expect(e.input, {'path': 'a.ts'});

    expect(ServerFrame.parse('{"type":"done","stopReason":"end_turn"}'), isA<DoneFrame>());
    expect(ServerFrame.parse('{"type":"error","message":"炸了"}'), isA<ErrorFrame>());
    expect(ServerFrame.parse('{"type":"permission_request","requestId":"r2"}'),
        isA<PermissionRequestFrame>());
    expect(ServerFrame.parse('{"type":"permission_resolved","requestId":"r2"}'),
        isA<PermissionResolvedFrame>());
    expect(ServerFrame.parse('{"type":"cleared"}'), isA<ClearedFrame>());
  });

  test('status without perm leaves it null', () {
    final frame = ServerFrame.parse('{"type":"status","busy":false}') as StatusFrame;
    expect(frame.perm, isNull);
    expect(frame.reply, '');
  });

  test('unknown or broken payloads degrade instead of throwing', () {
    expect(ServerFrame.parse('{"type":"task","x":1}'), isA<UnknownFrame>());
    expect(ServerFrame.parse('not json at all'), isA<UnknownFrame>());
    expect(ServerFrame.parse('{"type":"event"}'), isA<UnknownFrame>());
  });

  test('client frames serialize the way the server expects', () {
    expect(jsonDecode(jsonEncode(promptFrame('你好'))), {'type': 'prompt', 'text': '你好'});
    expect(jsonDecode(jsonEncode(cancelFrame())), {'type': 'cancel'});
    // `enabled` has no omitempty on the server struct: always send the bool.
    expect(jsonDecode(jsonEncode(autoFrame(false))), {'type': 'auto', 'enabled': false});
    expect(
      jsonDecode(jsonEncode(permissionFrame('r1', 'allow_once'))),
      {'type': 'permission', 'requestId': 'r1', 'optionId': 'allow_once'},
    );
  });
}
