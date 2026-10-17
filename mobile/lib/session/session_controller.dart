import 'package:flutter/foundation.dart';

import '../api/client.dart';
import '../api/endpoints.dart';
import '../api/types.dart';
import 'storage.dart';

enum SessionStatus { unknown, signedOut, signedIn }

/// Holds the signed-in state: base URL, session token, current user. The router
/// listens to it, so any state change re-evaluates redirects.
class SessionController extends ChangeNotifier {
  SessionController({required this.api, required this.storage});

  final ApiClient api;
  final SessionStorage storage;

  SessionStatus status = SessionStatus.unknown;
  User? user;

  RoundpenApi get endpoints => RoundpenApi(api);

  /// True when the active credential is an API key (`rp-...`) rather than a
  /// session token. Session tokens are opaque hex, so the prefix is unambiguous.
  /// API keys are durable account credentials, not sessions, so signing out must
  /// not try to revoke one server-side.
  bool get usesApiKey => api.token.startsWith('rp-');

  /// Loads the stored address and token and verifies the token against the
  /// server; anything rejected lands on the login screen.
  Future<void> restore() async {
    api.configure(baseUrl: await storage.readBaseUrl());
    final token = await storage.readToken();
    if (api.baseUrl.isEmpty || token.isEmpty) {
      status = SessionStatus.signedOut;
      notifyListeners();
      return;
    }
    api.configure(token: token);
    try {
      user = await endpoints.me();
      status = SessionStatus.signedIn;
    } on ApiError {
      await storage.clearToken();
      status = SessionStatus.signedOut;
    }
    notifyListeners();
  }

  /// Throws [ApiError] with a UI-ready message when the server rejects us.
  Future<void> signIn({
    required String server,
    required String username,
    required String password,
  }) async {
    api.configure(baseUrl: server, token: '');
    final res = await endpoints.login(username, password);
    if (res.sessionToken.isEmpty) {
      throw ApiError(0, '服务器没有返回会话 token（需要较新的 roundpend）');
    }
    api.configure(token: res.sessionToken);
    await storage.writeBaseUrl(api.baseUrl);
    await storage.writeToken(res.sessionToken);
    user = res.user;
    status = SessionStatus.signedIn;
    notifyListeners();
  }

  /// Signs in with an API key (`rp-...`) instead of a username/password. Used by
  /// fnOS-provisioned accounts, which have no local password. The key is stored
  /// through the same plumbing as a session token: both are just the credential
  /// the client sends as `Authorization: Bearer`.
  ///
  /// Throws [ApiError] with a UI-ready message when the server rejects the key.
  Future<void> signInWithApiKey({
    required String server,
    required String apiKey,
  }) async {
    api.configure(baseUrl: server, token: '');
    try {
      user = await endpoints.loginWithApiKey(apiKey);
    } on ApiError {
      api.configure(token: '');
      rethrow;
    }
    await storage.writeBaseUrl(api.baseUrl);
    await storage.writeToken(apiKey);
    status = SessionStatus.signedIn;
    notifyListeners();
  }

  Future<void> signOut() async {
    if (!usesApiKey) {
      try {
        await endpoints.logout();
      } on ApiError {
        // Local sign-out must succeed even if the server is unreachable; the
        // session then expires on its own.
      }
    }
    await _clear();
  }

  /// Wired into [ApiClient.onUnauthorized]: the server no longer accepts this
  /// token (revoked, expired, or password changed elsewhere).
  void handleUnauthorized() {
    if (status != SessionStatus.signedIn) return;
    _clear();
  }

  Future<void> _clear() async {
    api.configure(token: '');
    await storage.clearToken();
    user = null;
    status = SessionStatus.signedOut;
    notifyListeners();
  }
}
