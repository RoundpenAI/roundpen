import 'package:flutter_test/flutter_test.dart';
import 'package:roundpen_console/api/types.dart';
import 'package:roundpen_console/chat/chat_items.dart';
import 'package:roundpen_console/chat/chat_reducer.dart';
import 'package:roundpen_console/ws/frames.dart';

StatusFrame busyStatus({String reply = '', String thought = '', PermissionRequest? perm}) =>
    StatusFrame(busy: true, reply: reply, thought: thought, perm: perm);

EventFrame message(String text) =>
    EventFrame(AgentEvent(type: 'agent_message', text: text));

EventFrame thought(String text) =>
    EventFrame(AgentEvent(type: 'agent_thought', text: text));

EventFrame tool({
  required String id,
  String title = '',
  String status = '',
  String kind = '',
  Object? input,
  Object? output,
}) =>
    EventFrame(AgentEvent(
      type: 'tool_call',
      toolId: id,
      title: title,
      status: status,
      kind: kind,
      input: input,
      output: output,
    ));

void main() {
  test('message chunks append to the streaming reply', () {
    final r = ChatReducer();
    r.apply(message('你'));
    r.apply(message('好'));

    expect(r.state.streamingReply, '你好');
  });

  test('a status snapshot replaces the streamed text instead of appending', () {
    final r = ChatReducer();
    r.apply(message('半句'));
    r.apply(busyStatus(reply: '完整的一句话'));

    expect(r.state.streamingReply, '完整的一句话');
    expect(r.state.busy, isTrue);
  });

  test('a snapshot mid-stream with an empty reply clears what we streamed', () {
    final r = ChatReducer();
    r.apply(message('半句'));
    r.apply(const StatusFrame(busy: true));

    expect(r.state.streamingReply, '');
  });

  test('thought chunks extend one entry, and a snapshot replaces it', () {
    final r = ChatReducer();
    r.apply(thought('想'));
    r.apply(thought('一下'));
    expect((r.state.liveEntries.single as ThoughtActivity).text, '想一下');

    r.apply(busyStatus(thought: '重来'));
    expect((r.state.liveEntries.single as ThoughtActivity).text, '重来');
  });

  test('tools upsert by toolId and merge partial updates', () {
    final r = ChatReducer();
    r.apply(tool(id: 't1', title: 'Read', kind: 'read', input: {'path': 'a.ts'}));
    r.apply(tool(id: 't1', status: 'completed', output: 'done'));

    final entry = r.state.liveEntries.single as ToolActivity;
    expect(entry.toolId, 't1');
    expect(entry.call.title, 'Read'); // kept from the first frame
    expect(entry.call.input, {'path': 'a.ts'}); // kept
    expect(entry.call.status, 'completed'); // updated
    expect(entry.call.output, 'done'); // added
  });

  test('tools without a toolId still get their own row', () {
    final r = ChatReducer();
    r.apply(tool(id: '', title: 'Anonymous'));
    r.apply(tool(id: '', title: 'Another'));

    expect(r.state.liveEntries.length, 2);
  });

  test('permission requests open the sheet and matching resolutions close it', () {
    final r = ChatReducer();
    const request = PermissionRequest(requestId: 'r1', title: 'rm -rf');
    r.apply(const PermissionRequestFrame(request));
    expect(r.state.permission?.requestId, 'r1');

    r.apply(const PermissionResolvedFrame(requestId: 'r9'));
    expect(r.state.permission?.requestId, 'r1', reason: 'other ids must not close it');

    r.apply(const PermissionResolvedFrame(requestId: 'r1'));
    expect(r.state.permission, isNull);
  });

  test('a status snapshot without perm dismisses a stale prompt', () {
    final r = ChatReducer();
    r.apply(const PermissionRequestFrame(PermissionRequest(requestId: 'r1')));

    r.apply(const StatusFrame(busy: true));

    expect(r.state.permission, isNull);
  });

  test('done clears streaming state and reports the turn settled', () {
    var settled = 0;
    final r = ChatReducer(onTurnSettled: () => settled++);
    r.apply(message('回答'));
    r.apply(tool(id: 't1', title: 'Read'));
    r.apply(const DoneFrame(stopReason: 'end_turn'));

    expect(r.state.streamingReply, '');
    expect(r.state.liveEntries, isEmpty);
    expect(r.state.busy, isFalse);
    expect(settled, 1);
  });

  test('status busy:false after a busy turn also reports settled', () {
    var settled = 0;
    final r = ChatReducer(onTurnSettled: () => settled++);
    r.apply(busyStatus(reply: 'x'));
    r.apply(const StatusFrame(busy: false));

    expect(settled, 1);
  });

  test('errors keep the transcript and clear the streaming buffers', () {
    final r = ChatReducer();
    r.apply(const EventFrame(AgentEvent(type: 'agent_message', text: 'ok')));
    r.apply(const ErrorFrame(message: '炸了'));

    expect(r.state.error, '炸了');
    expect(r.state.streamingReply, '');
    expect(r.state.busy, isFalse);
  });

  test('cleared keeps the transcript and adds a divider', () {
    final r = ChatReducer();
    r.apply(const ClearedFrame());

    expect(r.state.items, hasLength(1));
    expect(r.state.items.single, isA<DividerItem>());
    expect(r.state.streamingReply, '');
  });

  test('unknown events degrade to a system line', () {
    final r = ChatReducer();
    r.apply(const EventFrame(AgentEvent(type: 'quantum_thing', text: '???')));

    expect(r.state.items.single, isA<SystemItem>());
    expect((r.state.items.single as SystemItem).text, '???');
  });

  test('plan events land in the activity block', () {
    final r = ChatReducer();
    r.apply(const EventFrame(AgentEvent(type: 'plan', text: '3 steps')));

    expect((r.state.liveEntries.single as ThoughtActivity).text, '3 steps');
  });

  group('history', () {
    test('groups consecutive thought and tool rows into one activity block', () {
      final items = itemsFromRows(const [
        MessageRow(id: 'm1', role: 'user', content: '你好'),
        MessageRow(id: 'm2', role: 'thought', content: '想想'),
        MessageRow(id: 'm3', role: 'tool', meta: {'toolId': 't1', 'title': 'Read'}),
        MessageRow(id: 'm4', role: 'tool', meta: {'toolId': 't2', 'title': 'Grep'}),
        MessageRow(id: 'm5', role: 'assistant', content: '做完了'),
      ]);

      expect(items, hasLength(3));
      expect(items[0], isA<UserItem>());
      final activity = items[1] as ActivityItem;
      expect(activity.toolCalls, hasLength(2));
      expect(activity.thoughtText, '想想');
      expect(items[2], isA<AssistantItem>());
    });

    test('renders command display text and clear markers', () {
      final items = itemsFromRows(const [
        MessageRow(id: 'm1', role: 'user', content: '展开后的指令', meta: {'display': '/review'}),
        MessageRow(id: 'm2', role: 'event', content: '', meta: {'type': 'clear'}),
      ]);

      expect((items[0] as UserItem).display, '/review');
      expect(items[1], isA<DividerItem>());
    });

    test('drops live tools the server has now persisted', () {
      final r = ChatReducer();
      r.apply(message('半句'));
      r.apply(tool(id: 't1', title: 'Read'));

      r.applyHistory(const [
        MessageRow(id: 'm1', role: 'tool', meta: {'toolId': 't1', 'title': 'Read'}),
      ]);

      expect(r.state.items, hasLength(1));
      expect(r.state.liveEntries, isEmpty, reason: 't1 is persisted now');
      // The streaming text stays until the turn settles.
      expect(r.state.streamingReply, '半句');
    });
  });
}
