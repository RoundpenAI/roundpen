import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

/// UI-facing error: the server's `{"message": ...}` when there is one, or a
/// transport failure with status 0.
class ApiError implements Exception {
  ApiError(this.status, this.message);

  final int status;
  final String message;

  @override
  String toString() => message;
}

/// Turns what a user types into a base URL: `192.168.1.5:19001` →
/// `http://192.168.1.5:19001`, trailing slashes stripped.
String normalizeBaseUrl(String raw) {
  var s = raw.trim();
  if (s.isEmpty) return '';
  if (!s.contains('://')) s = 'http://$s';
  while (s.endsWith('/')) {
    s = s.substring(0, s.length - 1);
  }
  return s;
}

/// Thin HTTP layer against the control plane. Every request carries the session
/// token as `Authorization: Bearer`; a 401 clears the session exactly once.
class ApiClient {
  ApiClient({http.Client Function()? clientFactory, this.timeout = const Duration(seconds: 20)})
      : _newClient = clientFactory ?? http.Client.new;

  final http.Client Function() _newClient;
  final Duration timeout;

  /// Server origin, normalized. Set through [configure] so user input cannot
  /// end up un-normalized here.
  String baseUrl = '';
  String token = '';
  void Function()? _onUnauthorized;

  void configure({String? baseUrl, String? token}) {
    if (baseUrl != null) this.baseUrl = normalizeBaseUrl(baseUrl);
    if (token != null) this.token = token;
  }

  void onUnauthorized(void Function() handler) => _onUnauthorized = handler;

  Future<dynamic> get(String path) => _send('GET', path);
  Future<dynamic> post(String path, {Object? body}) => _send('POST', path, body: body);
  Future<dynamic> patch(String path, {Object? body}) => _send('PATCH', path, body: body);
  Future<dynamic> delete(String path) => _send('DELETE', path);

  Future<dynamic> _send(String method, String path, {Object? body}) async {
    final client = _newClient();
    try {
      final req = http.Request(method, Uri.parse('$baseUrl$path'));
      req.headers['Accept'] = 'application/json';
      if (token.isNotEmpty) req.headers['Authorization'] = 'Bearer $token';
      if (body != null) {
        req.headers['Content-Type'] = 'application/json';
        req.body = jsonEncode(body);
      }
      final streamed = await client.send(req).timeout(timeout);
      final res = await http.Response.fromStream(streamed);
      // The control plane sends `application/json` without a charset (correct
      // per RFC 8259), which would make package:http fall back to latin1 and
      // mangle every non-ASCII character — decode the bytes ourselves.
      final responseBody = utf8.decode(res.bodyBytes);
      if (res.statusCode == 401) {
        _onUnauthorized?.call();
        throw ApiError(401, _messageOf(responseBody) ?? '登录已失效，请重新登录');
      }
      if (res.statusCode >= 400) {
        throw ApiError(res.statusCode, _messageOf(responseBody) ?? 'HTTP ${res.statusCode}');
      }
      if (responseBody.isEmpty) return null;
      return jsonDecode(responseBody);
    } on ApiError {
      rethrow;
    } on TimeoutException {
      throw ApiError(0, '连接超时，请检查服务器地址');
    } on SocketException {
      throw ApiError(0, '无法连接服务器，请检查地址与网络');
    } on http.ClientException {
      throw ApiError(0, '无法连接服务器，请检查地址与网络');
    } on FormatException {
      throw ApiError(0, '服务器返回了无法解析的内容');
    } finally {
      client.close();
    }
  }
}

String? _messageOf(String body) {
  if (body.isEmpty) return null;
  try {
    final decoded = jsonDecode(body);
    if (decoded is Map<String, dynamic>) {
      final msg = decoded['message'] ?? decoded['error'];
      if (msg is String && msg.isNotEmpty) return msg;
    }
  } on FormatException {
    return null;
  }
  return null;
}
