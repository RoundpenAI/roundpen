import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'pages/assistants_page.dart';
import 'pages/chat_page.dart';
import 'pages/login_page.dart';
import 'pages/sessions_page.dart';
import 'pages/settings_page.dart';
import 'session/session_controller.dart';

GoRouter buildRouter(SessionController session) {
  return GoRouter(
    initialLocation: '/splash',
    refreshListenable: session,
    redirect: (context, state) {
      final location = state.matchedLocation;
      switch (session.status) {
        case SessionStatus.unknown:
          return location == '/splash' ? null : '/splash';
        case SessionStatus.signedOut:
          return location == '/login' ? null : '/login';
        case SessionStatus.signedIn:
          return location == '/login' || location == '/splash' ? '/assistants' : null;
      }
    },
    routes: [
      GoRoute(
        path: '/splash',
        builder: (_, _) => const Scaffold(body: Center(child: CircularProgressIndicator())),
      ),
      GoRoute(path: '/login', builder: (_, _) => LoginPage(session: session)),
      GoRoute(path: '/settings', builder: (_, _) => SettingsPage(session: session)),
      GoRoute(path: '/assistants', builder: (_, _) => AssistantsPage(session: session)),
      GoRoute(
        path: '/assistants/:assistantId',
        builder: (_, state) => SessionsPage(
          session: session,
          assistantId: state.pathParameters['assistantId'] ?? '',
        ),
      ),
      GoRoute(
        path: '/assistants/:assistantId/sessions/:sessionId',
        builder: (_, state) => ChatPage(
          session: session,
          sessionId: state.pathParameters['sessionId'] ?? '',
        ),
      ),
    ],
  );
}
