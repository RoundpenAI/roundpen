import 'package:flutter/material.dart';

import 'api/client.dart';
import 'routes.dart';
import 'session/session_controller.dart';
import 'session/storage.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  final api = ApiClient();
  final session = SessionController(api: api, storage: SessionStorage());
  api.onUnauthorized(session.handleUnauthorized);
  runApp(RoundpenApp(session: session));
}

class RoundpenApp extends StatefulWidget {
  const RoundpenApp({super.key, required this.session});

  final SessionController session;

  @override
  State<RoundpenApp> createState() => _RoundpenAppState();
}

class _RoundpenAppState extends State<RoundpenApp> {
  late final router = buildRouter(widget.session);

  @override
  void initState() {
    super.initState();
    widget.session.restore();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'Roundpen 控制台',
      theme: ThemeData(colorSchemeSeed: const Color(0xFF3B6EA5), useMaterial3: true),
      darkTheme: ThemeData(
        colorSchemeSeed: const Color(0xFF3B6EA5),
        brightness: Brightness.dark,
        useMaterial3: true,
      ),
      routerConfig: router,
    );
  }
}
