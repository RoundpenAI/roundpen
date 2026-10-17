import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:roundpen_console/api/client.dart';
import 'package:roundpen_console/api/endpoints.dart';

ApiClient apiWith(MockClientHandler handler) {
  final api = ApiClient(clientFactory: () => MockClient(handler));
  api.baseUrl = 'http://box:19001';
  return api;
}

void main() {
  test('loginWithApiKey presents the rp- key as the Bearer credential', () async {
    late http.Request seen;
    final api = apiWith((req) async {
      seen = req;
      return http.Response('{"username":"fnos-1000","role":"user"}', 200);
    });

    final user = await RoundpenApi(api).loginWithApiKey('rp-abc123');

    expect(seen.headers['Authorization'], 'Bearer rp-abc123');
    expect(seen.url.path, '/v1/auth/user');
    expect(user.username, 'fnos-1000');
    // The client stays configured with the key so subsequent calls are authed.
    expect(api.token, 'rp-abc123');
  });

  test('loginWithApiKey surfaces a rejected key as ApiError', () async {
    final api = apiWith((_) async => http.Response('{"message":"unauthorized"}', 401));

    await expectLater(
      RoundpenApi(api).loginWithApiKey('rp-bad'),
      throwsA(isA<ApiError>().having((e) => e.status, 'status', 401)),
    );
  });
}
