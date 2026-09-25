import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Session token in the platform keystore; the (non-secret) server address in
/// ordinary preferences.
class SessionStorage {
  SessionStorage({FlutterSecureStorage? secure})
      : _secure = secure ?? const FlutterSecureStorage();

  static const _tokenKey = 'roundpen.sessionToken';
  static const _baseUrlKey = 'roundpen.baseUrl';

  final FlutterSecureStorage _secure;

  Future<String> readToken() async => await _secure.read(key: _tokenKey) ?? '';
  Future<void> writeToken(String value) => _secure.write(key: _tokenKey, value: value);
  Future<void> clearToken() => _secure.delete(key: _tokenKey);

  Future<String> readBaseUrl() async =>
      (await SharedPreferences.getInstance()).getString(_baseUrlKey) ?? '';
  Future<void> writeBaseUrl(String value) async =>
      (await SharedPreferences.getInstance()).setString(_baseUrlKey, value);
}
