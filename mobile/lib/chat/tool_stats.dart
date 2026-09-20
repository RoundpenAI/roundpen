/// Port of `web/src/lib/toolStats.ts`: turns a turn's tool calls into the
/// one-line summary shown on the activity block. Labels stay English so both
/// clients read the same and the ported tests assert the same strings.
library;

class ToolCall {
  const ToolCall({
    required this.title,
    this.status = '',
    this.kind = '',
    this.input,
    this.output,
  });

  final String title;
  final String status;
  final String kind;
  final Object? input;
  final Object? output;
}

enum ToolBucket { edit, explore, search, command, other }

class ToolGroupStats {
  const ToolGroupStats({required this.label, required this.plus, required this.minus});

  final String label;
  final int plus;
  final int minus;
}

const _pathKeys = [
  'path',
  'file',
  'file_path',
  'filePath',
  'target_file',
  'targetFile',
  'filename',
  'fileName',
  'dest',
  'destination',
];

final _editNames = RegExp(
    r'^(edit|write|strreplace|str_replace|search_replace|searchreplace|apply_patch|applypatch|notebookedit|notebook_edit|delete|unlink|rm|move|mv|rename)$');
final _exploreNames = RegExp(
    r'^(read|view|cat|glob|ls|list|listdir|list_dir|readdir|stat|webfetch|web_fetch|fetch)$');
final _searchNames = RegExp(r'^(grep|search|find|websearch|web_search|rg|ripgrep)$');
final _commandNames = RegExp(r'^(bash|shell|command|exec|terminal|run|sh)$');
final _listingNames = RegExp(r'^(glob|ls|list|listdir|list_dir|readdir)$');
final _commandTitlePrefix = RegExp(r'^(Bash|Shell|Command)\s*[·:]\s*', caseSensitive: false);

ToolBucket classifyTool(ToolCall call) {
  switch (call.kind.trim().toLowerCase()) {
    case 'edit':
    case 'delete':
    case 'move':
      return ToolBucket.edit;
    case 'read':
    case 'fetch':
      return ToolBucket.explore;
    case 'search':
      return ToolBucket.search;
    case 'execute':
      return ToolBucket.command;
  }

  final name = _toolName(call);
  if (_editNames.hasMatch(name)) return ToolBucket.edit;
  if (_exploreNames.hasMatch(name) || name.startsWith('browser_')) return ToolBucket.explore;
  if (_searchNames.hasMatch(name)) return ToolBucket.search;
  if (_commandNames.hasMatch(name)) return ToolBucket.command;
  return ToolBucket.other;
}

ToolGroupStats formatGroupStats(List<ToolCall> calls) {
  final order = <ToolBucket>[];
  final byBucket = <ToolBucket, _BucketAcc>{};
  var failed = 0;
  var plus = 0;
  var minus = 0;

  for (final call in calls) {
    if (call.status == 'failed') failed += 1;
    final bucket = classifyTool(call);
    final acc = byBucket.putIfAbsent(bucket, () {
      order.add(bucket);
      return _BucketAcc();
    });
    if (_isRunning(call.status)) {
      acc.running.add(call);
    } else {
      acc.done.add(call);
    }
    if (bucket == ToolBucket.edit) {
      final d = _diffForCall(call);
      plus += d.plus;
      minus += d.minus;
    }
  }

  final clauses = <String>[];
  for (final bucket in order) {
    clauses.addAll(_phraseBucket(bucket, byBucket[bucket]!));
  }
  if (clauses.isEmpty) {
    clauses.add('${calls.length} tool ${_plural(calls.length, 'call')}');
  }
  if (failed > 0) clauses.add('$failed failed');

  return ToolGroupStats(label: _joinClauses(clauses), plus: plus, minus: minus);
}

class _BucketAcc {
  final List<ToolCall> running = [];
  final List<ToolCall> done = [];
}

bool _isRunning(String status) => status == 'pending' || status == 'in_progress';

List<String> _phraseBucket(ToolBucket bucket, _BucketAcc acc) {
  switch (bucket) {
    case ToolBucket.edit:
      return _phraseCounted(
        acc,
        doneOne: (call) => 'Edited ${_fileLabel(call)}',
        doneMany: (n) => 'Edited $n files',
        runOne: (call) => 'Editing ${_fileLabel(call)}',
        runMany: (n) => 'Editing $n files',
        files: true,
      );
    case ToolBucket.explore:
      return _phraseExplore(acc);
    case ToolBucket.search:
      return _phraseCounted(
        acc,
        doneOne: (call) {
          final q = _queryLabel(call);
          return q.isNotEmpty ? 'Searched for $q' : '1 search';
        },
        doneMany: (n) => '$n searches',
        runOne: (call) {
          final q = _queryLabel(call);
          return q.isNotEmpty ? 'Searching for $q' : 'Searching';
        },
        runMany: (n) => 'Searching ($n)',
      );
    case ToolBucket.command:
      return _phraseCounted(
        acc,
        doneOne: (call) => 'Ran ${_commandLabel(call)}',
        doneMany: (n) => 'Ran $n commands',
        runOne: (call) => 'Running ${_commandLabel(call)}',
        runMany: (n) => 'Running $n commands',
      );
    case ToolBucket.other:
      return _phraseCounted(
        acc,
        doneOne: (call) => call.title.isNotEmpty ? call.title : 'Used a tool',
        doneMany: (n) => 'Used $n tools',
        runOne: (call) => call.title.isNotEmpty ? call.title : 'Working',
        runMany: (n) => 'Running $n tools',
      );
  }
}

List<String> _phraseExplore(_BucketAcc acc) {
  final all = [...acc.done, ...acc.running];
  if (all.isNotEmpty && all.every((c) => _toolName(c).startsWith('browser_'))) {
    return _phraseCounted(
      acc,
      doneOne: (call) {
        final url = _urlLabel(call);
        return url.isNotEmpty ? 'Opened $url' : 'Used the browser';
      },
      doneMany: (n) => 'Used the browser $n times',
      runOne: (call) {
        final url = _urlLabel(call);
        return url.isNotEmpty ? 'Opening $url' : 'Using the browser';
      },
      runMany: (n) => 'Using the browser ($n)',
    );
  }
  if (all.isNotEmpty && all.every((c) => _listingNames.hasMatch(_toolName(c)))) {
    return _phraseCounted(
      acc,
      doneOne: (call) {
        final p = _patternLabel(call);
        return p.isNotEmpty ? 'Listed $p' : 'Listed files';
      },
      doneMany: (n) => 'Listed $n times',
      runOne: (call) {
        final p = _patternLabel(call);
        return p.isNotEmpty ? 'Listing $p' : 'Listing files';
      },
      runMany: (n) => 'Listing files ($n)',
    );
  }
  return _phraseCounted(
    acc,
    doneOne: (call) => 'Read ${_fileLabel(call)}',
    doneMany: (n) => 'Explored $n files',
    runOne: (call) => 'Reading ${_fileLabel(call)}',
    runMany: (n) => 'Exploring $n files',
    files: true,
  );
}

List<String> _phraseCounted(
  _BucketAcc acc, {
  required String Function(ToolCall) doneOne,
  required String Function(int) doneMany,
  required String Function(ToolCall) runOne,
  required String Function(int) runMany,
  bool files = false,
}) {
  final doneN = files ? _countFiles(acc.done) : acc.done.length;
  final runN = files ? _countFiles(acc.running) : acc.running.length;
  final out = <String>[];
  if (doneN > 0) {
    out.add(doneN == 1 && acc.done.isNotEmpty ? doneOne(acc.done.first) : doneMany(doneN));
  }
  if (runN > 0) {
    out.add(runN == 1 && acc.running.isNotEmpty ? runOne(acc.running.first) : runMany(runN));
  }
  return out;
}

int _countFiles(List<ToolCall> calls) {
  final files = <String>{};
  var nameless = 0;
  for (final call in calls) {
    final paths = _collectPaths(call, classifyTool(call) == ToolBucket.explore);
    if (paths.isEmpty) {
      nameless += 1;
    } else {
      files.addAll(paths);
    }
  }
  return files.length + nameless;
}

String _fileLabel(ToolCall call) {
  final paths = _collectPaths(call, classifyTool(call) == ToolBucket.explore);
  if (paths.isNotEmpty) return _shortPath(paths.first);
  return 'a file';
}

String _shortPath(String path) {
  final clean = path.replaceAll('\\', '/').replaceAll(RegExp(r'/+$'), '');
  final parts = clean.split('/').where((p) => p.isNotEmpty).toList();
  final name = parts.isNotEmpty ? parts.last : clean;
  return _clip(name, 40);
}

String _queryLabel(ToolCall call) {
  final rec = _asRecord(call.input);
  final raw = _pickString(rec, ['pattern', 'query', 'regex', 'search', 'grep']) ?? '';
  return raw.isNotEmpty ? _clip(raw, 32) : '';
}

String _patternLabel(ToolCall call) {
  final rec = _asRecord(call.input);
  final raw = _pickString(rec, ['pattern', 'glob', 'glob_pattern', 'path', 'target']) ?? '';
  return raw.isNotEmpty ? _clip(raw, 36) : '';
}

String _commandLabel(ToolCall call) {
  final rec = _asRecord(call.input);
  final raw = _pickString(rec, ['command', 'cmd', 'script']) ??
      (rec['_'] is String ? rec['_'] as String : '');
  if (raw.isNotEmpty) {
    return _clip(raw.replaceAll(RegExp(r'\s+'), ' ').trim(), 42);
  }
  final title = call.title.replaceFirst(_commandTitlePrefix, '');
  return _clip(title.isNotEmpty ? title : 'a command', 42);
}

String _urlLabel(ToolCall call) {
  final rec = _asRecord(call.input);
  final raw = _pickString(rec, ['url', 'href', 'target']) ?? '';
  if (raw.isEmpty) return '';
  final host = Uri.tryParse(raw)?.host ?? '';
  return _clip(host.isNotEmpty ? host : raw, 36);
}

Map<String, dynamic> _asRecord(Object? value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return value.cast<String, dynamic>();
  return const {};
}

String _clip(String value, int max) =>
    value.length > max ? '${value.substring(0, max)}…' : value;

String _joinClauses(List<String> parts) {
  if (parts.isEmpty) return '';
  final rest = parts.skip(1).map((p) => p.isEmpty ? p : p[0].toLowerCase() + p.substring(1));
  return [parts.first, ...rest].join(', ');
}

String _toolName(ToolCall call) {
  final raw = call.title.trim();
  final first = raw.isEmpty ? '' : raw.split(RegExp(r'[\s:·]+')).first;
  return first.toLowerCase().replaceAll(RegExp(r'[^a-z0-9_]'), '');
}

List<String> _collectPaths(ToolCall call, bool includeOutputList) {
  final found = <String>{};
  _addPathsFromValue(call.input, found);
  if (includeOutputList) _addOutputPathList(call.output, found);
  return found.toList();
}

void _addPathsFromValue(Object? value, Set<String> into) {
  if (value == null) return;
  if (value is String) {
    final p = _normalizePath(value);
    if (p.isNotEmpty && _looksLikePath(p) && !p.contains('\n')) into.add(p);
    return;
  }
  if (value is List) {
    for (final item in value) {
      _addPathsFromValue(item, into);
    }
    return;
  }
  if (value is! Map) return;
  final rec = _asRecord(value);
  for (final key in _pathKeys) {
    final v = rec[key];
    if (v is String) {
      final p = _normalizePath(v);
      if (p.isNotEmpty) into.add(p);
    } else if (v is List) {
      for (final item in v) {
        if (item is String) {
          final p = _normalizePath(item);
          if (p.isNotEmpty) into.add(p);
        }
      }
    }
  }
  final paths = rec['paths'];
  if (paths is List) {
    for (final item in paths) {
      if (item is String) {
        final p = _normalizePath(item);
        if (p.isNotEmpty) into.add(p);
      }
    }
  }
}

void _addOutputPathList(Object? output, Set<String> into) {
  final text = _outputText(output);
  if (text.isEmpty) return;
  final lines = text.split(RegExp(r'\r?\n'));
  if (lines.length > 80) return;
  for (final line in lines) {
    final trimmed = line.trim();
    if (trimmed.isEmpty || trimmed.startsWith('{') || trimmed.startsWith('[')) continue;
    if (RegExp(r'\s').hasMatch(trimmed)) continue;
    if (!_looksLikePath(trimmed)) continue;
    final p = _normalizePath(trimmed);
    if (p.isNotEmpty) into.add(p);
  }
}

String _outputText(Object? output) {
  if (output == null) return '';
  if (output is String) return output;
  if (output is Map) {
    final rec = _asRecord(output);
    for (final key in ['content', 'text', 'output', 'result', 'stdout']) {
      final v = rec[key];
      if (v is String) return v;
    }
  }
  return '';
}

String _normalizePath(String raw) => raw.trim().replaceAll('\\', '/');

bool _looksLikePath(String value) {
  if (value.isEmpty || value.length > 512) return false;
  if (value.startsWith('/') || value.startsWith('./') || value.startsWith('../')) return true;
  return RegExp(r'[\\/]').hasMatch(value) || RegExp(r'\.[a-zA-Z0-9]{1,8}$').hasMatch(value);
}

class _Diff {
  const _Diff(this.plus, this.minus);
  final int plus;
  final int minus;
}

_Diff _diffForCall(ToolCall call) {
  final fromStrings = _stringsDiff(call.input);
  if (fromStrings != null) return fromStrings;
  final fromOutput = _unifiedDiff(_outputText(call.output));
  if (fromOutput.plus > 0 || fromOutput.minus > 0) return fromOutput;
  return const _Diff(0, 0);
}

_Diff? _stringsDiff(Object? input) {
  if (input is! Map) return null;
  final rec = _asRecord(input);
  final oldStr = _pickString(rec, ['old_string', 'oldString', 'old_str', 'oldText']);
  final newStr = _pickString(rec, ['new_string', 'newString', 'new_str', 'newText']);
  if (oldStr != null && newStr != null) return _lineDelta(oldStr, newStr);

  final patch = _pickString(rec, ['patch', 'diff', 'unified_diff']);
  if (patch != null && patch.isNotEmpty) {
    final d = _unifiedDiff(patch);
    if (d.plus > 0 || d.minus > 0) return d;
  }
  return null;
}

String? _pickString(Map<String, dynamic> rec, List<String> keys) {
  for (final key in keys) {
    final v = rec[key];
    if (v is String) return v;
  }
  return null;
}

_Diff _lineDelta(String oldText, String newText) {
  final oldLines = oldText.split('\n');
  final newLines = newText.split('\n');
  final bag = <String, int>{};
  for (final line in oldLines) {
    bag[line] = (bag[line] ?? 0) + 1;
  }
  var common = 0;
  for (final line in newLines) {
    final n = bag[line] ?? 0;
    if (n > 0) {
      bag[line] = n - 1;
      common += 1;
    }
  }
  return _Diff(newLines.length - common, oldLines.length - common);
}

_Diff _unifiedDiff(String text) {
  var plus = 0;
  var minus = 0;
  for (final line in text.split(RegExp(r'\r?\n'))) {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('@@')) continue;
    if (line.startsWith('+')) {
      plus += 1;
    } else if (line.startsWith('-')) {
      minus += 1;
    }
  }
  return _Diff(plus, minus);
}

String _plural(int n, String one, [String? many]) =>
    n == 1 ? one : (many ?? '${one}s');
