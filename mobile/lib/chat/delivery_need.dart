/// Port of `web/src/lib/deliveryNeed.ts`: infer Commit→PR / Merge actions
/// from session tool history.
library;

import 'tool_stats.dart';

class DeliveryNeed {
  const DeliveryNeed({this.openPR = false, this.mergePR = false});

  final bool openPR;
  final bool mergePR;

  bool get hasNeed => openPR || mergePR;
}

/// Suggested prompts — same wording as web DeliveryActionBar.
const deliveryPromptOpenPR =
    'Commit any pending changes with a conventional message, push the feature branch '
    '(never the default branch, never force-push), and open a pull request with '
    'CreatePullRequest. Always open a PR — do not push directly to the default branch. '
    'SoftDeny confirmations are expected for push/PR.';

const deliveryPromptMergePR =
    'Merge the open pull request with MergePullRequest using squash. Do not force-push. '
    'Wait for SoftDeny confirmation.';

/// Infer delivery actions from completed tool calls in the session.
DeliveryNeed inferDeliveryNeed(List<ToolCall> calls) {
  final dirtyRepos = <String>{};
  final openPRs = <String>{};

  for (final call in calls) {
    final status = call.status.toLowerCase();
    if (status == 'pending' || status == 'in_progress') continue;
    final ok = status != 'failed' && status != 'error' && status != 'cancelled';
    final name = _toolName(call);
    final repo = _repoFromCall(call);

    if (_isEditLike(call)) {
      if (!ok) continue;
      final path = _pathFromCall(call);
      final r = repo ?? (path != null ? repoFromPath(path) : null);
      if (r != null) dirtyRepos.add(r);
      continue;
    }

    if (name == 'gitcommit') {
      if (ok && repo != null) dirtyRepos.remove(repo);
      continue;
    }
    if (name == 'createpullrequest') {
      if (ok && repo != null) {
        dirtyRepos.remove(repo);
        openPRs.add(repo);
      }
      continue;
    }
    if (name == 'mergepullrequest') {
      if (ok && repo != null) openPRs.remove(repo);
      continue;
    }
  }

  return DeliveryNeed(openPR: dirtyRepos.isNotEmpty, mergePR: openPRs.isNotEmpty);
}

/// Map a workspace path to its project repo root (`projects/<slug>`), if any.
String? repoFromPath(String path) {
  final p = _normalizePath(path);
  if (p.isEmpty) return null;
  final m = RegExp(r'^(?:workspace/)?projects/([^/]+)').firstMatch(p);
  if (m != null) return 'projects/${m.group(1)}';
  return null;
}

/// SoftDeny / always-confirm delivery tools (mirrors automode RequiresConfirm).
bool isSoftDenyToolTitle(String title) {
  final name = _nameFromTitle(title);
  return const {
    'bash',
    'gitpush',
    'createpullrequest',
    'mergepullrequest',
    'postpullrequestreview',
    'browser_type',
    'browser_click',
    'browser_evaluate',
  }.contains(name);
}

String _toolName(ToolCall call) => _nameFromTitle(call.title);

String _nameFromTitle(String title) {
  final raw = title.trim();
  final first = raw.isEmpty ? '' : raw.split(RegExp(r'[\s:·]+')).first;
  return first.toLowerCase().replaceAll(RegExp(r'[^a-z0-9_]'), '');
}

bool _isEditLike(ToolCall call) {
  final kind = call.kind.trim().toLowerCase();
  if (kind == 'edit' || kind == 'delete' || kind == 'move') return true;
  return RegExp(
    r'^(edit|write|strreplace|str_replace|search_replace|searchreplace|apply_patch|applypatch|notebookedit|notebook_edit|delete|unlink|rm|move|mv|rename|create|create_file)$',
  ).hasMatch(_toolName(call));
}

String? _pathFromCall(ToolCall call) {
  final rec = _asRecord(call.input);
  final fromInput = _pickString(rec, [
    'path',
    'file',
    'file_path',
    'filePath',
    'target_file',
    'targetFile',
  ]);
  if (fromInput != null) return _normalizePath(fromInput);
  final m = RegExp(r'[·:]\s*(.+)$').firstMatch(call.title);
  final cand = (m?.group(1) ?? '').trim();
  if (cand.isEmpty || cand.contains('\n')) return null;
  if (!RegExp(r'[\\/]').hasMatch(cand) &&
      !RegExp(r'\.[a-zA-Z0-9]{1,8}$').hasMatch(cand)) {
    return null;
  }
  return _normalizePath(cand);
}

String? _repoFromCall(ToolCall call) {
  final rec = _asRecord(call.input);
  final raw = _pickString(rec, ['repo_path', 'repoPath', 'cwd', 'work_dir', 'workdir']);
  if (raw != null) return repoFromPath(raw) ?? _normalizePath(raw);
  final m = RegExp(r'[·:]\s*(.+)$').firstMatch(call.title);
  if (m?.group(1) != null) return repoFromPath(m!.group(1)!.trim());
  return null;
}

Map<String, dynamic> _asRecord(Object? value) {
  if (value is Map) return value.cast<String, dynamic>();
  return const {};
}

String? _pickString(Map<String, dynamic> rec, List<String> keys) {
  for (final key in keys) {
    final v = rec[key];
    if (v is String && v.trim().isNotEmpty) return v.trim();
  }
  return null;
}

String _normalizePath(String path) {
  var p = path.trim().replaceAll('\\', '/');
  while (p.startsWith('./')) {
    p = p.substring(2);
  }
  if (p.startsWith('/workspace/')) {
    p = p.substring('/workspace/'.length);
  } else if (p.startsWith('workspace/')) {
    p = p.substring('workspace/'.length);
  } else if (p.startsWith('/')) {
    p = p.substring(1);
  }
  return p;
}
