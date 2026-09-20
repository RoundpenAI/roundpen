import 'client.dart';
import 'types.dart';

/// Typed wrapper over the endpoints the mobile console needs (first phase:
/// auth, assistants, sessions).
class RoundpenApi {
  RoundpenApi(this.client);

  final ApiClient client;

  /// Logs in and asks the server to hand back the freshly issued session token
  /// (the database only stores API-key hashes, so this is the only way a native
  /// client can obtain a durable credential).
  Future<({User user, String sessionToken})> login(String user, String password) async {
    final res = await client.post('/v1/auth/login', body: {
      'user': user,
      'password': password,
      'returnSessionToken': true,
    }) as Map<String, dynamic>;
    return (
      user: User.fromJson(res['user'] as Map<String, dynamic>),
      sessionToken: res['sessionToken'] as String? ?? '',
    );
  }

  Future<User> me() async {
    final res = await client.get('/v1/auth/user') as Map<String, dynamic>;
    return User.fromJson(res);
  }

  /// Revokes the session server-side (Bearer-authenticated requests carry no
  /// cookie, so the handler uses the session id from the request context).
  Future<void> logout() => client.post('/v1/auth/logout');

  Future<List<Assistant>> assistants() async {
    final res = await client.get('/v1/assistants') as Map<String, dynamic>;
    final rows = res['assistants'] as List? ?? const [];
    return rows.map((e) => Assistant.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// All of the user's sessions (server caps at the newest 50 and does not
  /// filter by assistant — callers filter client-side).
  Future<List<AgentSession>> sessions() async {
    final res = await client.get('/v1/agent-sessions') as Map<String, dynamic>;
    final rows = res['sessions'] as List? ?? const [];
    return rows.map((e) => AgentSession.fromJson(e as Map<String, dynamic>)).toList();
  }

  Future<List<MessageRow>> messages(String sessionId) async {
    final res = await client.get('/v1/agent-sessions/$sessionId/messages')
        as Map<String, dynamic>;
    final rows = res['messages'] as List? ?? const [];
    return rows.map((e) => MessageRow.fromJson(e as Map<String, dynamic>)).toList();
  }

  /// Resolves an assist ticket opened by a permission request (best effort —
  /// the WS answer is what actually unblocks the turn).
  Future<void> resolveTicket(String ticketId, String resolution, String note) =>
      client.post('/v1/assist-tickets/$ticketId/resolve', body: {
        'resolution': resolution,
        'note': note,
      });

  Future<AgentSession> getSession(String id) async {
    final res = await client.get('/v1/agent-sessions/$id') as Map<String, dynamic>;
    return AgentSession.fromJson(res);
  }

  Future<AgentSession> createSession({required String assistantId, String title = '新会话'}) async {
    final res = await client.post('/v1/agent-sessions', body: {
      'title': title,
      'assistantId': assistantId,
    }) as Map<String, dynamic>;
    return AgentSession.fromJson(res);
  }

  Future<AgentSession> renameSession(String id, String title) async {
    final res = await client.patch('/v1/agent-sessions/$id', body: {'title': title})
        as Map<String, dynamic>;
    return AgentSession.fromJson(res);
  }

  Future<void> deleteSession(String id) => client.delete('/v1/agent-sessions/$id');

  /// Returns the assistant's current session, starting one if needed.
  Future<String> ensureSession(String assistantId) async {
    final res = await client.post('/v1/assistants/$assistantId/ensure-session') as Map<String, dynamic>;
    return res['sessionId'] as String? ?? '';
  }
}
