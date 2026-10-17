# Shared helpers for fnOS lifecycle scripts (install / upgrade / main).
# Sourced, not executed.

pkg_user() { printf '%s\n' "${TRIM_USERNAME:-roundpen}"; }
pkg_group() { printf '%s\n' "${TRIM_GROUPNAME:-roundpen}"; }

is_root() { [ "$(id -u)" -eq 0 ]; }

# Make $TRIM_PKGVAR owned by the package user so roundpend can read
# data/secret.key (0600). Older Docker-packaged installs left these files
# root-owned; after switching to run-as=package the daemon could not open them.
ensure_pkgvar_perms() {
	local root="${TRIM_PKGVAR:?}"
	local user group
	user="$(pkg_user)"
	group="$(pkg_group)"
	mkdir -p "${root}/data" "${root}/.pg0" || return 1
	if ! is_root; then
		return 0
	fi
	chown -R "${user}:${group}" "${root}" || return 1
	if [ -f "${root}/data/secret.key" ]; then
		chmod 0600 "${root}/data/secret.key" || true
	fi
	if [ -d "${root}/data/install-identity" ]; then
		chmod 0700 "${root}/data/install-identity" || true
		# Private key material under install-identity must stay owner-only.
		find "${root}/data/install-identity" -type f -exec chmod 0600 {} + 2>/dev/null || true
	fi
	return 0
}

# Run a command as the package user when the lifecycle script is root.
# Long-running services should not stay root (see config/privilege).
#
# Prefer setpriv --init-groups so join-groups from config/privilege (e.g.
# docker) are actually in the process credential set. runuser -u/-g alone has
# been observed on fnOS to drop supplementary groups, which yields
# permission denied on /var/run/docker.sock.
run_as_pkg() {
	local user group
	user="$(pkg_user)"
	group="$(pkg_group)"
	if is_root && command -v setpriv >/dev/null 2>&1; then
		setpriv --reuid="${user}" --regid="${group}" --init-groups -- "$@"
	elif is_root && command -v runuser >/dev/null 2>&1; then
		# No -g: let runuser load the account's full group set (incl. docker).
		runuser -u "${user}" --preserve-environment -- "$@"
	elif is_root; then
		echo "runuser/setpriv missing; cannot drop privileges to ${user}" >&2
		return 1
	else
		"$@"
	fi
}
