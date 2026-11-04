# Shared helpers for fnOS lifecycle scripts (install / upgrade / main).
# Sourced, not executed.

pkg_user() { printf '%s\n' "${TRIM_USERNAME:-roundpen}"; }
pkg_group() { printf '%s\n' "${TRIM_GROUPNAME:-roundpen}"; }

is_root() { [ "$(id -u)" -eq 0 ]; }

# Prepare $TRIM_PKGVAR for the package user (run-as=package). Soften modes on
# files we own. If leftover root-owned data (old Docker packages) is unreadable,
# fail so the operator can chown manually — lifecycle no longer runs as root.
ensure_pkgvar_perms() {
	local root="${TRIM_PKGVAR:?}"
	local user group
	user="$(pkg_user)"
	group="$(pkg_group)"
	mkdir -p "${root}/data" "${root}/.pg0" || return 1

	# Defensive: if somehow invoked as root (local tests), reclaim ownership.
	if is_root; then
		chown -R "${user}:${group}" "${root}" || return 1
	fi

	if [ -f "${root}/data/secret.key" ]; then
		if [ ! -r "${root}/data/secret.key" ]; then
			return 1
		fi
		chmod 0600 "${root}/data/secret.key" 2>/dev/null || true
	fi
	if [ -d "${root}/data/install-identity" ]; then
		if [ ! -r "${root}/data/install-identity" ] || [ ! -x "${root}/data/install-identity" ]; then
			return 1
		fi
		chmod 0700 "${root}/data/install-identity" 2>/dev/null || true
		# Private key material under install-identity must stay owner-only.
		find "${root}/data/install-identity" -type f -exec chmod 0600 {} + 2>/dev/null || true
	fi
	return 0
}

# Default OCI images for Agent / Browser slots (keep in sync with
# internal/config and template seeds).
default_agent_image() { printf '%s\n' "${ROUNDPEN_AGENT_IMAGE:-ghcr.io/roundpenai/code-agent:0.1.0}"; }
default_browser_image() { printf '%s\n' "${ROUNDPEN_BROWSER_IMAGE:-ghcr.io/browserless/chrome:v2.56.7}"; }

# Pull one image; on failure try China-friendly ghcr mirrors (daemon.json
# registry-mirrors do not apply to ghcr.io). Retag to the logical name.
pull_image_with_mirrors() {
	local ref="$1"
	local candidate path
	if docker pull "${ref}"; then
		return 0
	fi
	case "${ref}" in
	ghcr.io/*)
		path="${ref#ghcr.io/}"
		for candidate in \
			"ghcr.1ms.run/${path}" \
			"ghcr.nju.edu.cn/${path}" \
			"m.daocloud.io/${ref}" \
			"ghcr.m.daocloud.io/${path}"; do
			echo "[warm-images] retry via ${candidate}"
			if docker pull "${candidate}"; then
				docker tag "${candidate}" "${ref}" || return 1
				return 0
			fi
		done
		;;
	esac
	# Optional operator templates: ROUNDPEN_IMAGE_MIRRORS=host/{image},host2/{path}
	if [ -n "${ROUNDPEN_IMAGE_MIRRORS:-}" ] && [ "${ROUNDPEN_IMAGE_MIRRORS}" != "none" ] && [ "${ROUNDPEN_IMAGE_MIRRORS}" != "off" ]; then
		local tmpl IFS=','
		path="${ref#*/}"
		for tmpl in ${ROUNDPEN_IMAGE_MIRRORS}; do
			tmpl="$(printf '%s' "${tmpl}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
			[ -n "${tmpl}" ] || continue
			candidate="$(printf '%s' "${tmpl}" | sed -e "s|{image}|${ref}|g" -e "s|{path}|${path}|g")"
			echo "[warm-images] retry via ${candidate}"
			if docker pull "${candidate}"; then
				docker tag "${candidate}" "${ref}" || return 1
				return 0
			fi
		done
	fi
	return 1
}

# Kick off a background docker pull for Agent + Browser images so the first
# browser_navigate after install does not wait on a multi-GB download inside
# the tool call. Never blocks install/start; failures only go to the log.
warm_docker_images() {
	local logfile="${1:-}"
	local agent browser
	agent="$(default_agent_image)"
	browser="$(default_browser_image)"
	if ! command -v docker >/dev/null 2>&1; then
		return 0
	fi
	(
		set +e
		echo "[warm-images] pulling ${browser}"
		pull_image_with_mirrors "${browser}" || echo "[warm-images] browser pull failed"
		echo "[warm-images] pulling ${agent}"
		pull_image_with_mirrors "${agent}" || echo "[warm-images] agent pull failed"
		echo "[warm-images] done"
	) >>"${logfile:-/dev/null}" 2>&1 &
	return 0
}

# Run a command as the package user. With run-as=package the lifecycle already
# is that user, so this is a no-op. Kept for local tests that still invoke
# scripts as root.
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
