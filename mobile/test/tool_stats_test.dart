import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/chat/tool_stats.dart';

/// Ported case-for-case from `web/src/lib/toolStats.test.ts`.
void main() {
  group('classifyTool', () {
    test('prefers the ACP kind over the title', () {
      expect(
        classifyTool(const ToolCall(title: 'Bash', status: 'completed', kind: 'read')),
        ToolBucket.explore,
      );
    });

    test('maps titles when kind is missing', () {
      expect(classifyTool(const ToolCall(title: 'Edit', status: 'completed')), ToolBucket.edit);
      expect(
        classifyTool(const ToolCall(title: 'Read src/a.ts', status: 'completed')),
        ToolBucket.explore,
      );
      expect(classifyTool(const ToolCall(title: 'Grep', status: 'completed')), ToolBucket.search);
      expect(classifyTool(const ToolCall(title: 'Bash', status: 'completed')), ToolBucket.command);
    });
  });

  group('formatGroupStats', () {
    test('aggregates a finished mixed turn', () {
      final calls = <ToolCall>[
        for (var i = 0; i < 18; i++)
          ToolCall(
            title: 'Edit',
            status: 'completed',
            kind: 'edit',
            input: {
              'path': 'src/f$i.ts',
              'old_string': 'keep\nold',
              'new_string': 'keep\nnew\nextra',
            },
          ),
        for (var i = 0; i < 8; i++)
          ToolCall(
            title: 'Read',
            status: 'completed',
            kind: 'read',
            input: {'path': 'src/r$i.ts'},
          ),
        const ToolCall(title: 'Grep', status: 'completed', kind: 'search'),
        const ToolCall(title: 'Grep', status: 'completed', kind: 'search'),
        const ToolCall(title: 'Grep', status: 'completed', kind: 'search'),
        const ToolCall(title: 'Bash', status: 'completed', kind: 'execute'),
        const ToolCall(title: 'Bash', status: 'completed', kind: 'execute'),
      ];

      final stats = formatGroupStats(calls);

      expect(stats.label, 'Edited 18 files, explored 8 files, 3 searches, ran 2 commands');
      expect(stats.plus, 36);
      expect(stats.minus, 18);
    });

    test('names the file while a write is still running', () {
      final stats = formatGroupStats(const [
        ToolCall(
          title: 'Edit',
          status: 'in_progress',
          kind: 'edit',
          input: {'path': 'src/a.ts', 'old_string': 'x', 'new_string': 'y'},
        ),
      ]);

      expect(stats.label, 'Editing a.ts');
      expect(stats.plus, 1);
      expect(stats.minus, 1);
    });

    test('switches a finished single edit to Edited', () {
      final stats = formatGroupStats(const [
        ToolCall(
          title: 'Edit',
          status: 'completed',
          kind: 'edit',
          input: {'path': 'src/a.ts', 'old_string': 'x', 'new_string': 'y'},
        ),
      ]);

      expect(stats.label, 'Edited a.ts');
    });

    test('keeps finished work in the past while a later read is still going', () {
      final stats = formatGroupStats(const [
        ToolCall(
          title: 'Edit',
          status: 'completed',
          kind: 'edit',
          input: {'path': 'a.ts', 'old_string': 'x', 'new_string': 'y'},
        ),
        ToolCall(
          title: 'Read',
          status: 'in_progress',
          kind: 'read',
          input: {'path': 'b.ts'},
        ),
      ]);

      expect(stats.label, 'Edited a.ts, reading b.ts');
    });

    test('names a running command and a search query', () {
      final stats = formatGroupStats(const [
        ToolCall(
          title: 'Grep',
          status: 'in_progress',
          kind: 'search',
          input: {'pattern': 'handleClick'},
        ),
        ToolCall(
          title: 'Bash',
          status: 'in_progress',
          kind: 'execute',
          input: {'command': 'go test ./internal/acp/...'},
        ),
      ]);

      expect(stats.label, 'Searching for handleClick, running go test ./internal/acp/...');
    });

    test('dedupes reads and notes a failed command', () {
      final stats = formatGroupStats(const [
        ToolCall(title: 'Read', status: 'completed', input: {'path': 'a.ts'}),
        ToolCall(title: 'Read', status: 'completed', input: {'path': 'a.ts'}),
        ToolCall(
          title: 'Bash',
          status: 'failed',
          kind: 'execute',
          input: {'command': 'make'},
        ),
      ]);

      expect(stats.label, 'Read a.ts, ran make, 1 failed');
    });
  });
}
