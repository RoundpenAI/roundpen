import 'package:flutter/material.dart';

/// Input row: send while idle, stop while the agent is working.
class Composer extends StatefulWidget {
  const Composer({
    super.key,
    required this.busy,
    required this.onSend,
    required this.onStop,
    this.hint,
  });

  final bool busy;
  final void Function(String text) onSend;
  final VoidCallback onStop;
  final String? hint;

  @override
  State<Composer> createState() => _ComposerState();
}

class _ComposerState extends State<Composer> {
  final _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _send() {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    _controller.clear();
    widget.onSend(text);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 6, 12, 10),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (widget.hint != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Text(widget.hint!, style: theme.textTheme.labelSmall),
              ),
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                Expanded(
                  child: TextField(
                    controller: _controller,
                    minLines: 1,
                    maxLines: 5,
                    textInputAction: TextInputAction.send,
                    onSubmitted: (_) => _send(),
                    decoration: InputDecoration(
                      hintText: '给助手发消息…',
                      border: const OutlineInputBorder(),
                      contentPadding:
                          const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                      isDense: true,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                widget.busy
                    ? IconButton.filledTonal(
                        onPressed: widget.onStop,
                        tooltip: '停止',
                        icon: const Icon(Icons.stop),
                      )
                    : IconButton.filled(
                        onPressed: _send,
                        tooltip: '发送',
                        icon: const Icon(Icons.send),
                      ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
