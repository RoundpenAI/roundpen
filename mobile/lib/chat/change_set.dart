/// Port of `web/src/lib/changeSet.ts`: aggregate Edit/Write tool calls into a
/// foldable file-change set for the session transcript.
library;

import 'tool_stats.dart';

const foldLineThreshold = 80;

enum FileChangeKind { edit, write, patch }

class FileChange {
  const FileChange({
    required this.path,
    required this.unified,
    required this.plus,
    required this.minus,
    required this.kind,
  });

  final String path;
  final String unified;
  final int plus;
  final int minus;
  final FileChangeKind kind;
}

class ChangeSet {
  const ChangeSet({required this.files, required this.plus, required this.minus});

  final List<FileChange> files;
  final int plus;
  final int minus;
}

bool shouldFoldDiff(String unified) =>
    unified.split(RegExp(r'\r?\n')).length > foldLineThreshold;

/// Build a ChangeSet from completed Edit/Write tool calls in an activity group.
ChangeSet? buildChangeSet(List<ToolCall> calls) {
  final byPath = <String, FileChange>{};
  for (final call in calls) {
    if (call.status == 'failed' ||
        call.status == 'pending' ||
        call.status == 'in_progress') {
      continue;
    }
    if (!_isEditLike(call)) continue;
    final change = _fileChangeFromCall(call);
    if (change == null) continue;
    final prev = byPath[change.path];
    if (prev == null) {
      byPath[change.path] = change;
      continue;
    }
    byPath[change.path] = FileChange(
      path: change.path,
      unified: change.unified.isNotEmpty ? change.unified : prev.unified,
      plus: prev.plus + change.plus,
      minus: prev.minus + change.minus,
      kind: change.kind,
    );
  }
  final files = byPath.values.toList()
    ..sort((a, b) => a.path.compareTo(b.path));
  if (files.isEmpty) return null;
  var plus = 0;
  var minus = 0;
  for (final f in files) {
    plus += f.plus;
    minus += f.minus;
  }
  return ChangeSet(files: files, plus: plus, minus: minus);
}

/// Workspace URL path that opens the Files page and previews this path (web).
String workspacePreviewHref(String path) {
  final clean = path.replaceFirst(RegExp(r'^/+'), '').replaceAll('\\', '/');
  final parts = clean.split('/');
  final name = parts.isEmpty ? clean : parts.removeLast();
  final dir = parts.isEmpty ? '.' : parts.join('/');
  final q = Uri(queryParameters: {'path': dir, 'preview': name}).query;
  return '/workspace?$q';
}

/// Absolute web console URL for a workspace preview, when [baseUrl] is known.
String? workspacePreviewUrl(String baseUrl, String path) {
  final base = baseUrl.trim();
  if (base.isEmpty) return null;
  final origin = base.endsWith('/') ? base.substring(0, base.length - 1) : base;
  return '$origin${workspacePreviewHref(path)}';
}

FileChange? _fileChangeFromCall(ToolCall call) {
  final rec = _asRecord(call.input);
  final path = _pickString(rec, [
        'path',
        'file',
        'file_path',
        'filePath',
        'target_file',
        'targetFile',
      ]) ??
      _pathFromTitle(call.title);
  if (path == null || path.isEmpty) return null;

  final oldStr = _pickString(rec, ['old_string', 'oldString', 'old_str', 'oldText']);
  final newStr = _pickString(rec, ['new_string', 'newString', 'new_str', 'newText']);
  if (oldStr != null && newStr != null) {
    final unified = toUnifiedDiff(path, oldStr, newStr);
    final counts = countUnified(unified);
    return FileChange(
      path: path,
      unified: unified,
      plus: counts.plus,
      minus: counts.minus,
      kind: FileChangeKind.edit,
    );
  }

  final contents = _pickString(rec, ['contents', 'content', 'text', 'data']);
  if (contents != null && _isWriteTool(call)) {
    final unified = toUnifiedDiff(path, '', contents);
    final counts = countUnified(unified);
    return FileChange(
      path: path,
      unified: unified,
      plus: counts.plus,
      minus: counts.minus,
      kind: FileChangeKind.write,
    );
  }

  final patch = _pickString(rec, ['patch', 'diff', 'unified_diff']);
  if (patch != null && patch.isNotEmpty) {
    final counts = countUnified(patch);
    return FileChange(
      path: path,
      unified: patch,
      plus: counts.plus,
      minus: counts.minus,
      kind: FileChangeKind.patch,
    );
  }

  final out = _outputText(call.output);
  if (out.contains('\n+++ ') || out.contains('\n@@ ')) {
    final counts = countUnified(out);
    return FileChange(
      path: path,
      unified: out,
      plus: counts.plus,
      minus: counts.minus,
      kind: FileChangeKind.patch,
    );
  }
  return null;
}

String toUnifiedDiff(String path, String oldText, String newText) {
  final oldLines = oldText.isEmpty ? <String>[] : oldText.split('\n');
  final newLines = newText.isEmpty ? <String>[] : newText.split('\n');
  final lines = <String>[
    '--- a/$path',
    '+++ b/$path',
    '@@ -1,${oldLines.isEmpty ? 1 : oldLines.length} +1,${newLines.isEmpty ? 1 : newLines.length} @@',
    for (final line in oldLines) '-$line',
    for (final line in newLines) '+$line',
  ];
  return lines.join('\n');
}

({int plus, int minus}) countUnified(String text) {
  var plus = 0;
  var minus = 0;
  for (final line in text.split(RegExp(r'\r?\n'))) {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('@@')) {
      continue;
    }
    if (line.startsWith('+')) {
      plus += 1;
    } else if (line.startsWith('-')) {
      minus += 1;
    }
  }
  return (plus: plus, minus: minus);
}

bool _isWriteTool(ToolCall call) {
  final name = _toolName(call);
  return name == 'write' || name == 'create' || name == 'create_file';
}

bool _isEditLike(ToolCall call) {
  final kind = call.kind.trim().toLowerCase();
  if (kind == 'edit' || kind == 'delete' || kind == 'move') return true;
  return RegExp(
    r'^(edit|write|strreplace|str_replace|search_replace|searchreplace|apply_patch|applypatch|notebookedit|notebook_edit|delete|unlink|rm|move|mv|rename|create|create_file)$',
  ).hasMatch(_toolName(call));
}

String _toolName(ToolCall call) {
  final raw = call.title.trim();
  final first = raw.isEmpty ? '' : raw.split(RegExp(r'[\s:·]+')).first;
  return first.toLowerCase().replaceAll(RegExp(r'[^a-z0-9_]'), '');
}

String? _pathFromTitle(String title) {
  final m = RegExp(r'[·:]\s*(.+)$').firstMatch(title);
  final cand = (m?.group(1) ?? '').trim();
  if (cand.isEmpty || cand.contains('\n')) return null;
  if (!RegExp(r'[\\/]').hasMatch(cand) &&
      !RegExp(r'\.[a-zA-Z0-9]{1,8}$').hasMatch(cand)) {
    return null;
  }
  return cand.replaceAll('\\', '/');
}

Map<String, dynamic> _asRecord(Object? value) {
  if (value is Map) return value.cast<String, dynamic>();
  return const {};
}

String? _pickString(Map<String, dynamic> rec, List<String> keys) {
  for (final key in keys) {
    final v = rec[key];
    if (v is String) return v;
  }
  return null;
}

String _outputText(Object? output) {
  if (output == null) return '';
  if (output is String) return output;
  if (output is Map) {
    final rec = output.cast<String, dynamic>();
    for (final key in ['content', 'text', 'output', 'result', 'stdout']) {
      final v = rec[key];
      if (v is String) return v;
    }
  }
  return '';
}
