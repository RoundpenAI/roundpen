import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/chat/change_set.dart';
import 'package:roundpen_console/chat/tool_stats.dart';

void main() {
  group('buildChangeSet', () {
    test('aggregates Edit old/new into a file change', () {
      final cs = buildChangeSet([
        ToolCall(
          title: 'Edit',
          status: 'completed',
          kind: 'edit',
          input: {
            'path': 'projects/demo/a.ts',
            'old_string': 'const x = 1\n',
            'new_string': 'const x = 2\nconst y = 3\n',
          },
        ),
      ]);
      expect(cs, isNotNull);
      expect(cs!.files.length, 1);
      expect(cs.files.first.path, 'projects/demo/a.ts');
      expect(cs.files.first.unified.contains('--- a/projects/demo/a.ts'), isTrue);
      expect(cs.plus >= 1, isTrue);
    });

    test('skips running and non-edit tools', () {
      final cs = buildChangeSet([
        const ToolCall(
          title: 'Edit',
          status: 'in_progress',
          input: {'path': 'a.ts', 'old_string': 'a', 'new_string': 'b'},
        ),
        const ToolCall(
          title: 'Read',
          status: 'completed',
          input: {'path': 'a.ts'},
        ),
      ]);
      expect(cs, isNull);
    });

    test('merges multiple edits of the same path', () {
      final cs = buildChangeSet([
        const ToolCall(
          title: 'Edit',
          status: 'completed',
          input: {'path': 'f.go', 'old_string': 'a', 'new_string': 'b'},
        ),
        const ToolCall(
          title: 'Edit',
          status: 'completed',
          input: {'path': 'f.go', 'old_string': 'b', 'new_string': 'c\nd'},
        ),
      ]);
      expect(cs, isNotNull);
      expect(cs!.files.length, 1);
      expect(cs.files.first.unified.contains('+c'), isTrue);
    });
  });

  group('toUnifiedDiff / fold / href', () {
    test('builds a readable hunk', () {
      final u = toUnifiedDiff('x.ts', 'old\n', 'new\n');
      expect(u.contains('-old'), isTrue);
      expect(u.contains('+new'), isTrue);
    });

    test('folds large diffs', () {
      final lines = List.generate(100, (i) => '+line $i');
      expect(shouldFoldDiff(lines.join('\n')), isTrue);
      expect(shouldFoldDiff('+a\n-b\n'), isFalse);
    });

    test('builds workspace preview deep links', () {
      expect(
        workspacePreviewHref('projects/foo/bar.ts'),
        '/workspace?path=projects%2Ffoo&preview=bar.ts',
      );
      expect(workspacePreviewHref('README.md'), '/workspace?path=.&preview=README.md');
      expect(
        workspacePreviewUrl('https://nas.example:19001', 'projects/foo/bar.ts'),
        'https://nas.example:19001/workspace?path=projects%2Ffoo&preview=bar.ts',
      );
    });
  });
}
