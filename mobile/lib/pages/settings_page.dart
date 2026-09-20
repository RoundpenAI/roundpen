import 'package:flutter/material.dart';

import '../session/session_controller.dart';

class SettingsPage extends StatelessWidget {
  const SettingsPage({super.key, required this.session});

  final SessionController session;

  @override
  Widget build(BuildContext context) {
    final user = session.user;
    return Scaffold(
      appBar: AppBar(title: const Text('设置')),
      body: ListView(
        children: [
          ListTile(
            leading: const Icon(Icons.dns_outlined),
            title: const Text('服务器'),
            subtitle: Text(session.api.baseUrl.isEmpty ? '未设置' : session.api.baseUrl),
          ),
          ListTile(
            leading: const Icon(Icons.person_outline),
            title: const Text('账号'),
            subtitle: Text(
              user == null
                  ? '未登录'
                  : '${user.username}${user.isAdmin ? '（管理员）' : ''}',
            ),
          ),
          const Divider(),
          ListTile(
            leading: const Icon(Icons.logout),
            title: const Text('退出登录'),
            subtitle: const Text('在服务端吊销本设备的会话；重新登录时可修改服务器地址'),
            onTap: () => session.signOut(),
          ),
        ],
      ),
    );
  }
}
