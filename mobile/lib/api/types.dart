/// Wire types mirroring the control plane's JSON (see internal/storage and
/// internal/assistant). Only the fields the mobile console uses are kept.
class User {
  const User({required this.username, this.email = '', this.role = 'user'});

  final String username;
  final String email;
  final String role;

  bool get isAdmin => role == 'admin';

  factory User.fromJson(Map<String, dynamic> json) => User(
        username: json['username'] as String? ?? '',
        email: json['email'] as String? ?? '',
        role: json['role'] as String? ?? 'user',
      );
}

class Assistant {
  const Assistant({required this.id, required this.name, this.bio = ''});

  final String id;
  final String name;
  final String bio;

  factory Assistant.fromJson(Map<String, dynamic> json) => Assistant(
        id: json['id'] as String? ?? '',
        name: json['name'] as String? ?? '',
        bio: json['bio'] as String? ?? '',
      );
}

/// One persisted transcript row. `meta` carries the type-specific payload:
/// tools (`toolId/title/status/kind/input/output`), permissions, `clear`
/// markers, command display text, …
class MessageRow {
  const MessageRow({
    required this.id,
    required this.role,
    this.content = '',
    this.meta = const {},
  });

  final String id;
  final String role;
  final String content;
  final Map<String, dynamic> meta;

  factory MessageRow.fromJson(Map<String, dynamic> json) => MessageRow(
        id: json['id'] as String? ?? '',
        role: json['role'] as String? ?? '',
        content: json['content'] as String? ?? '',
        meta: (json['meta'] as Map?)?.cast<String, dynamic>() ?? const {},
      );
}

class AgentSession {
  const AgentSession({
    required this.id,
    required this.title,
    this.assistantId = '',
    this.status = '',
  });

  final String id;
  final String title;
  final String assistantId;
  final String status;

  factory AgentSession.fromJson(Map<String, dynamic> json) => AgentSession(
        id: json['id'] as String? ?? '',
        title: json['title'] as String? ?? '',
        assistantId: json['assistantId'] as String? ?? '',
        status: json['status'] as String? ?? '',
      );
}
