import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:roundpen_console/api/client.dart';
import 'package:roundpen_console/pages/sessions_page.dart';
import 'package:roundpen_console/session/session_controller.dart';
import 'package:roundpen_console/session/storage.dart';

/// Mirrors the real control plane exactly: JSON bytes with
/// `Content-Type: application/json` and no charset (so non-ASCII text only
/// survives if the client decodes UTF-8 itself).
http.Response jsonResponse(Object data, [int status = 200]) => http.Response.bytes(
      utf8.encode(jsonEncode(data)),
      status,
      headers: {'content-type': 'application/json'},
    );

SessionController controllerReturning(List<Map<String, String>> sessions) {
  final api = ApiClient(
    clientFactory: () => MockClient((req) async {
      if (req.url.path == '/v1/agent-sessions') {
        return jsonResponse({'sessions': sessions});
      }
      return jsonResponse({'message': 'not found'}, 404);
    }),
  );
  return SessionController(api: api, storage: SessionStorage());
}

Widget wrap(Widget child) => MaterialApp(home: child);

void main() {
  testWidgets('only the current assistant\'s sessions are listed', (tester) async {
    final session = controllerReturning(const [
      {'id': 's1', 'title': 'a1 的会话', 'assistantId': 'a1'},
      {'id': 's2', 'title': 'a2 的会话', 'assistantId': 'a2'},
      {'id': 's3', 'title': 'a1 的第二个', 'assistantId': 'a1'},
    ]);

    await tester.pumpWidget(wrap(SessionsPage(session: session, assistantId: 'a1')));
    await tester.pumpAndSettle();

    expect(find.text('a1 的会话'), findsOneWidget);
    expect(find.text('a1 的第二个'), findsOneWidget);
    expect(find.text('a2 的会话'), findsNothing);
  });

  testWidgets('an assistant without sessions shows the empty state', (tester) async {
    final session = controllerReturning(const [
      {'id': 's2', 'title': 'a2 的会话', 'assistantId': 'a2'},
    ]);

    await tester.pumpWidget(wrap(SessionsPage(session: session, assistantId: 'a1')));
    await tester.pumpAndSettle();

    expect(find.text('这个助手还没有会话，点右下角新建'), findsOneWidget);
  });

  testWidgets('server errors surface as a retryable message', (tester) async {
    final api = ApiClient(
      clientFactory: () => MockClient((_) async => jsonResponse({'message': '服务器开小差'}, 500)),
    );
    final session = SessionController(api: api, storage: SessionStorage());

    await tester.pumpWidget(wrap(SessionsPage(session: session, assistantId: 'a1')));
    await tester.pumpAndSettle();

    expect(find.text('服务器开小差'), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);
  });
}
