import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:roundpen_console/api/client.dart';

ApiClient clientWith(MockClientHandler handler, {void Function()? onUnauthorized}) {
  final client = ApiClient(clientFactory: () => MockClient(handler));
  client.baseUrl = 'http://box:19001';
  client.token = 'tok';
  if (onUnauthorized != null) client.onUnauthorized(onUnauthorized);
  return client;
}

void main() {
  group('normalizeBaseUrl', () {
    test('adds http:// when the scheme is missing', () {
      expect(normalizeBaseUrl('192.168.1.5:19001'), 'http://192.168.1.5:19001');
    });

    test('trims whitespace and trailing slashes', () {
      expect(normalizeBaseUrl('  http://box:19001/  '), 'http://box:19001');
    });

    test('keeps an explicit https scheme', () {
      expect(normalizeBaseUrl('https://rp.example.com'), 'https://rp.example.com');
    });

    test('empty input stays empty', () {
      expect(normalizeBaseUrl('   '), '');
    });
  });

  test('sends the session token as a Bearer credential', () async {
    late http.Request seen;
    final client = clientWith((req) async {
      seen = req;
      return http.Response('{"username":"bob"}', 200);
    });

    await client.get('/v1/auth/user');

    expect(seen.headers['Authorization'], 'Bearer tok');
    expect(seen.url.toString(), 'http://box:19001/v1/auth/user');
  });

  test('decodes JSON bodies', () async {
    final client = clientWith((_) async => http.Response('{"sessions":[]}', 200));
    expect(await client.get('/v1/agent-sessions'), {'sessions': []});
  });

  test('401 notifies the handler exactly once and throws', () async {
    var calls = 0;
    final client = clientWith(
      (_) async => http.Response('{"message":"unauthorized"}', 401),
      onUnauthorized: () => calls++,
    );

    await expectLater(
      client.get('/v1/auth/user'),
      throwsA(isA<ApiError>().having((e) => e.status, 'status', 401)),
    );
    expect(calls, 1);
  });

  test('error bodies surface the server message', () async {
    final client = clientWith(
      (_) async => http.Response(jsonEncode({'message': 'invalid user or password'}), 400),
    );

    await expectLater(
      client.post('/v1/auth/login', body: {'user': 'bob'}),
      throwsA(isA<ApiError>()
          .having((e) => e.status, 'status', 400)
          .having((e) => e.message, 'message', 'invalid user or password')),
    );
  });

  test('decodes UTF-8 bodies from a charset-less JSON response', () async {
    // The control plane sends `application/json` without a charset; package:http
    // would decode that as latin1 and mangle every non-ASCII character.
    final client = clientWith((_) async => http.Response.bytes(
          utf8.encode(jsonEncode({'message': '会话已失效'})),
          403,
          headers: {'content-type': 'application/json'},
        ));

    await expectLater(
      client.get('/v1/auth/user'),
      throwsA(isA<ApiError>().having((e) => e.message, 'message', '会话已失效')),
    );
  });

  test('serializes request bodies and sets the content type', () async {
    late http.Request seen;
    final client = clientWith((req) async {
      seen = req;
      return http.Response('{}', 200);
    });

    await client.post('/v1/auth/login', body: {'user': 'bob', 'returnSessionToken': true});

    expect(seen.headers['Content-Type'], startsWith('application/json'));
    expect(jsonDecode(seen.body), {'user': 'bob', 'returnSessionToken': true});
  });

  test('an empty body decodes to null', () async {
    final client = clientWith((_) async => http.Response('', 204));
    expect(await client.post('/v1/auth/logout'), isNull);
  });
}
