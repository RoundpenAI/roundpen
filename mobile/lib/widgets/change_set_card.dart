import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../chat/change_set.dart';

/// Read-only folded ChangeSet — mobile port of `web/src/components/ChangeSetCard.tsx`.
/// No in-app file preview: "网页打开" copies the web `/workspace?path=&preview=` URL.
class ChangeSetCard extends StatefulWidget {
  const ChangeSetCard({
    super.key,
    required this.changeSet,
    this.webBaseUrl = '',
    this.title,
  });

  final ChangeSet changeSet;
  final String webBaseUrl;
  final String? title;

  @override
  State<ChangeSetCard> createState() => _ChangeSetCardState();
}

class _ChangeSetCardState extends State<ChangeSetCard> {
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final n = widget.changeSet.files.length;
    final heading = widget.title ?? (n == 1 ? '1 file changed' : '$n files changed');

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      child: Theme(
        data: theme.copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          initiallyExpanded: true,
          tilePadding: const EdgeInsets.symmetric(horizontal: 12),
          childrenPadding: const EdgeInsets.only(bottom: 8),
          leading: Icon(Icons.difference_outlined, size: 18, color: theme.colorScheme.primary),
          title: Text.rich(
            TextSpan(
              style: theme.textTheme.bodySmall,
              children: [
                TextSpan(text: heading),
                if (widget.changeSet.plus > 0)
                  TextSpan(
                    text: ' +${widget.changeSet.plus}',
                    style: TextStyle(color: theme.colorScheme.tertiary),
                  ),
                if (widget.changeSet.minus > 0)
                  TextSpan(
                    text: ' -${widget.changeSet.minus}',
                    style: TextStyle(color: theme.colorScheme.error),
                  ),
              ],
            ),
          ),
          children: [
            for (final file in widget.changeSet.files)
              _FileChangeRow(
                file: file,
                webBaseUrl: widget.webBaseUrl,
              ),
          ],
        ),
      ),
    );
  }
}

class _FileChangeRow extends StatefulWidget {
  const _FileChangeRow({required this.file, required this.webBaseUrl});

  final FileChange file;
  final String webBaseUrl;

  @override
  State<_FileChangeRow> createState() => _FileChangeRowState();
}

class _FileChangeRowState extends State<_FileChangeRow> {
  late bool _expanded;

  @override
  void initState() {
    super.initState();
    _expanded = !shouldFoldDiff(widget.file.unified);
  }

  Future<void> _copyWebLink() async {
    final url = workspacePreviewUrl(widget.webBaseUrl, widget.file.path) ??
        workspacePreviewHref(widget.file.path);
    await Clipboard.setData(ClipboardData(text: url));
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          widget.webBaseUrl.isEmpty
              ? '已复制网页路径（登录控制台后打开）'
              : '已复制网页预览链接',
        ),
        duration: const Duration(seconds: 2),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final lines = widget.file.unified.split(RegExp(r'\r?\n'));

    return Padding(
      padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: InkWell(
                  onTap: () => setState(() => _expanded = !_expanded),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(vertical: 6, horizontal: 4),
                    child: Text.rich(
                      TextSpan(
                        style: theme.textTheme.labelMedium?.copyWith(
                          fontFamily: 'monospace',
                        ),
                        children: [
                          TextSpan(
                            text: _expanded ? '▾ ' : '▸ ',
                            style: theme.textTheme.labelMedium,
                          ),
                          TextSpan(text: widget.file.path),
                          if (widget.file.plus > 0)
                            TextSpan(
                              text: ' +${widget.file.plus}',
                              style: TextStyle(
                                color: theme.colorScheme.tertiary,
                                fontFamily: null,
                              ),
                            ),
                          if (widget.file.minus > 0)
                            TextSpan(
                              text: ' -${widget.file.minus}',
                              style: TextStyle(
                                color: theme.colorScheme.error,
                                fontFamily: null,
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
              TextButton(
                onPressed: _copyWebLink,
                child: const Text('网页打开'),
              ),
            ],
          ),
          if (_expanded)
            Container(
              width: double.infinity,
              constraints: const BoxConstraints(maxHeight: 280),
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: theme.colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(8),
              ),
              child: SingleChildScrollView(
                child: SelectableText.rich(
                  TextSpan(
                    style: theme.textTheme.labelSmall?.copyWith(
                      fontFamily: 'monospace',
                      height: 1.35,
                    ),
                    children: [
                      for (final line in lines)
                        TextSpan(
                          text: '$line\n',
                          style: TextStyle(color: _diffColor(theme, line)),
                        ),
                    ],
                  ),
                ),
              ),
            )
          else
            TextButton(
              onPressed: () => setState(() => _expanded = true),
              child: Text('Large diff folded — show ${lines.length} lines'),
            ),
        ],
      ),
    );
  }

  Color? _diffColor(ThemeData theme, String line) {
    if (line.startsWith('+') && !line.startsWith('+++')) {
      return theme.colorScheme.tertiary;
    }
    if (line.startsWith('-') && !line.startsWith('---')) {
      return theme.colorScheme.error;
    }
    if (line.startsWith('@@')) {
      return theme.colorScheme.primary;
    }
    return null;
  }
}
