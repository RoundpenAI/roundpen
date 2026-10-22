import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/chat/delivery_need.dart';
import 'package:roundpen_console/chat/tool_stats.dart';

void main() {
  group('inferDeliveryNeed', () {
    test('offers open PR after edits under projects/', () {
      final need = inferDeliveryNeed([
        const ToolCall(
          title: 'Edit',
          status: 'completed',
          kind: 'edit',
          input: {
            'path': 'projects/demo/a.ts',
            'old_string': 'a',
            'new_string': 'b',
          },
        ),
      ]);
      expect(need.openPR, isTrue);
      expect(need.mergePR, isFalse);
    });

    test('clears dirty after CreatePullRequest and offers merge', () {
      final need = inferDeliveryNeed([
        const ToolCall(
          title: 'Edit',
          status: 'completed',
          input: {'path': 'projects/demo/a.ts', 'old_string': 'a', 'new_string': 'b'},
        ),
        const ToolCall(
          title: 'CreatePullRequest',
          status: 'completed',
          input: {'repo_path': 'projects/demo'},
        ),
      ]);
      expect(need.openPR, isFalse);
      expect(need.mergePR, isTrue);
    });

    test('clears merge after MergePullRequest', () {
      final need = inferDeliveryNeed([
        const ToolCall(
          title: 'CreatePullRequest',
          status: 'completed',
          input: {'repo_path': 'projects/demo'},
        ),
        const ToolCall(
          title: 'MergePullRequest',
          status: 'completed',
          input: {'repo_path': 'projects/demo', 'number': 1},
        ),
      ]);
      expect(need.hasNeed, isFalse);
    });
  });

  group('isSoftDenyToolTitle', () {
    test('flags delivery tools', () {
      expect(isSoftDenyToolTitle('GitPush'), isTrue);
      expect(isSoftDenyToolTitle('CreatePullRequest · projects/foo'), isTrue);
      expect(isSoftDenyToolTitle('MergePullRequest'), isTrue);
      expect(isSoftDenyToolTitle('Edit'), isFalse);
    });
  });
}
