import 'dart:convert';

/// WebSocket frames exchanged with the control plane. The server side is
/// `internal/api/agentapi/handler.go` (wsIn/wsOut) and `runner.go`
/// (statusSnapshot); this file is the client's single source of truth for them.

/// One ACP event, as carried inside a `event` frame.
class AgentEvent {
  const AgentEvent({
    required this.type,
    this.text = '',
    this.title = '',
    this.status = '',
    this.kind = '',
    this.toolId = '',
    this.input,
    this.output,
  });

  final String type;
  final String text;
  final String title;
  final String status;
  final String kind;
  final String toolId;
  final Object? input;
  final Object? output;

  factory AgentEvent.fromJson(Map<String, dynamic> json) => AgentEvent(
        type: json['type'] as String? ?? '',
        text: json['text'] as String? ?? '',
        title: json['title'] as String? ?? '',
        status: json['status'] as String? ?? '',
        kind: json['kind'] as String? ?? '',
        toolId: json['toolId'] as String? ?? '',
        input: json['input'],
        output: json['output'],
      );
}

class PermissionOption {
  const PermissionOption({required this.optionId, this.name = '', this.kind = ''});

  final String optionId;
  final String name;
  final String kind;

  factory PermissionOption.fromJson(Map<String, dynamic> json) => PermissionOption(
        optionId: json['optionId'] as String? ?? '',
        name: json['name'] as String? ?? '',
        kind: json['kind'] as String? ?? '',
      );
}

class PermissionRequest {
  const PermissionRequest({
    required this.requestId,
    this.title = '',
    this.options = const [],
    this.ticketId = '',
  });

  final String requestId;
  final String title;
  final List<PermissionOption> options;
  final String ticketId;

  factory PermissionRequest.fromJson(Map<String, dynamic> json) => PermissionRequest(
        requestId: json['requestId'] as String? ?? '',
        title: json['title'] as String? ?? '',
        options: (json['options'] as List? ?? const [])
            .map((e) => PermissionOption.fromJson(e as Map<String, dynamic>))
            .toList(),
        ticketId: json['ticketId'] as String? ?? '',
      );
}

/// Server → client. Unknown types degrade to [UnknownFrame] instead of throwing,
/// so a newer server never crashes an older app.
sealed class ServerFrame {
  const ServerFrame();

  static ServerFrame parse(String raw) {
    final Object? decoded;
    try {
      decoded = jsonDecode(raw);
    } on FormatException {
      return UnknownFrame(type: '', raw: raw);
    }
    if (decoded is! Map<String, dynamic>) return UnknownFrame(type: '', raw: raw);

    final type = decoded['type'] as String? ?? '';
    switch (type) {
      case 'hello':
        return HelloFrame(message: decoded['message'] as String? ?? '');
      case 'status':
        final perm = decoded['perm'];
        return StatusFrame(
          busy: decoded['busy'] as bool? ?? false,
          reply: decoded['reply'] as String? ?? '',
          thought: decoded['thought'] as String? ?? '',
          perm: perm is Map<String, dynamic> ? PermissionRequest.fromJson(perm) : null,
        );
      case 'event':
        final event = decoded['event'];
        if (event is! Map<String, dynamic>) return UnknownFrame(type: type, raw: raw);
        return EventFrame(AgentEvent.fromJson(event));
      case 'done':
        return DoneFrame(stopReason: decoded['stopReason'] as String? ?? '');
      case 'error':
        return ErrorFrame(message: decoded['message'] as String? ?? '');
      case 'permission_request':
        return PermissionRequestFrame(PermissionRequest.fromJson(decoded));
      case 'permission_resolved':
        return PermissionResolvedFrame(requestId: decoded['requestId'] as String? ?? '');
      case 'cleared':
        return const ClearedFrame();
      default:
        return UnknownFrame(type: type, raw: raw);
    }
  }
}

class HelloFrame extends ServerFrame {
  const HelloFrame({required this.message});
  final String message;
}

/// Authoritative snapshot: `reply`/`thought` are the accumulated buffers of the
/// in-flight turn and must REPLACE whatever the client streamed so far.
class StatusFrame extends ServerFrame {
  const StatusFrame({
    required this.busy,
    this.reply = '',
    this.thought = '',
    this.perm,
  });

  final bool busy;
  final String reply;
  final String thought;
  final PermissionRequest? perm;
}

class EventFrame extends ServerFrame {
  const EventFrame(this.event);
  final AgentEvent event;
}

class DoneFrame extends ServerFrame {
  const DoneFrame({this.stopReason = ''});
  final String stopReason;
}

class ErrorFrame extends ServerFrame {
  const ErrorFrame({required this.message});
  final String message;
}

class PermissionRequestFrame extends ServerFrame {
  const PermissionRequestFrame(this.request);
  final PermissionRequest request;
}

class PermissionResolvedFrame extends ServerFrame {
  const PermissionResolvedFrame({required this.requestId});
  final String requestId;
}

class ClearedFrame extends ServerFrame {
  const ClearedFrame();
}

class UnknownFrame extends ServerFrame {
  const UnknownFrame({required this.type, required this.raw});
  final String type;
  final String raw;
}

/// Client → server frames.
Map<String, Object?> promptFrame(String text) => {'type': 'prompt', 'text': text};

Map<String, Object?> cancelFrame() => {'type': 'cancel'};

Map<String, Object?> autoFrame(bool enabled) => {'type': 'auto', 'enabled': enabled};

Map<String, Object?> permissionFrame(String requestId, String optionId) => {
      'type': 'permission',
      'requestId': requestId,
      'optionId': optionId,
    };
