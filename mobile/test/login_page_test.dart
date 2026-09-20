import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/api/client.dart';
import 'package:roundpen_console/pages/login_page.dart';
import 'package:roundpen_console/session/session_controller.dart';
import 'package:roundpen_console/session/storage.dart';

void main() {
  testWidgets('login page renders the three fields', (tester) async {
    final session = SessionController(api: ApiClient(), storage: SessionStorage());
    await tester.pumpWidget(MaterialApp(home: LoginPage(session: session)));

    expect(find.text('服务器地址'), findsOneWidget);
    expect(find.text('用户名'), findsOneWidget);
    expect(find.text('密码'), findsOneWidget);
    expect(find.text('登录'), findsOneWidget);
  });

  testWidgets('empty submit is rejected without a request', (tester) async {
    final session = SessionController(api: ApiClient(), storage: SessionStorage());
    await tester.pumpWidget(MaterialApp(home: LoginPage(session: session)));

    await tester.tap(find.text('登录'));
    await tester.pump();

    expect(find.text('服务器地址、用户名、密码都要填'), findsOneWidget);
  });
}
