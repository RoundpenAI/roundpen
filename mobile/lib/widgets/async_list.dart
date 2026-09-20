import 'package:flutter/material.dart';

import '../api/client.dart';

/// FutureBuilder + pull-to-refresh + empty/error states, shared by the list
/// pages so they behave the same.
class AsyncList<T> extends StatelessWidget {
  const AsyncList({
    super.key,
    required this.future,
    required this.onRefresh,
    required this.emptyText,
    required this.itemBuilder,
  });

  final Future<List<T>> future;
  final Future<void> Function() onRefresh;
  final String emptyText;
  final Widget Function(BuildContext context, T item) itemBuilder;

  @override
  Widget build(BuildContext context) {
    return RefreshIndicator(
      onRefresh: onRefresh,
      child: FutureBuilder<List<T>>(
        future: future,
        builder: (context, snapshot) {
          if (snapshot.connectionState == ConnectionState.waiting) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError) {
            final error = snapshot.error;
            return _Message(
              text: error is ApiError ? error.message : '$error',
              onRetry: onRefresh,
            );
          }
          final items = snapshot.data ?? const [];
          if (items.isEmpty) {
            return _Message(text: emptyText);
          }
          return ListView.separated(
            physics: const AlwaysScrollableScrollPhysics(),
            itemCount: items.length,
            separatorBuilder: (_, _) => const Divider(height: 1),
            itemBuilder: (context, i) => itemBuilder(context, items[i]),
          );
        },
      ),
    );
  }
}

/// Keeps pull-to-refresh working in empty/error states.
class _Message extends StatelessWidget {
  const _Message({required this.text, this.onRetry});

  final String text;
  final Future<void> Function()? onRetry;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: constraints.maxHeight),
          child: Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Padding(
                  padding: const EdgeInsets.all(24),
                  child: Text(text, textAlign: TextAlign.center),
                ),
                if (onRetry != null)
                  FilledButton.tonal(onPressed: onRetry, child: const Text('重试')),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
