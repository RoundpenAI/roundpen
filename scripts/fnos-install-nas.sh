#!/usr/bin/env bash
# Install/upgrade Roundpen on the fnOS NAS while preserving folder grants.
#
# appcenter-cli has no true "upgrade" for sideloaded fpks: install-local stops,
# uninstalls, then reinstalls (new app_id). That wipes app_authorization_path /
# app_user_authorization_path. We snapshot those rows, reinstall, restore them,
# then restart so TRIM_DATA_ACCESSIBLE_PATHS is reinjected.
#
# Usage:
#   ./scripts/fnos-install-nas.sh <version>
#   ./scripts/fnos-install-nas.sh 2.5.5
#
# Expects dist/fnos/roundpen-<version>-fnos-x86.fpk locally.
# Target: mike@10.10.1.3 (override with FNOS_HOST).
set -euo pipefail

VERSION="${1:?usage: $0 <version>}"
HOST="${FNOS_HOST:-mike@10.10.1.3}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FPK="${ROOT}/dist/fnos/roundpen-${VERSION}-fnos-x86.fpk"
REMOTE_FPK="/tmp/roundpen-${VERSION}-fnos-x86.fpk"
REMOTE_TPK="/vol1/appcenter-downloads/roundpen-${VERSION}-tpk"

[[ -f "${FPK}" ]] || {
	echo "missing ${FPK} — run: VERSION=${VERSION} make fpk" >&2
	exit 1
}

echo "==> scp ${FPK} -> ${HOST}:${REMOTE_FPK}"
scp "${FPK}" "${HOST}:${REMOTE_FPK}"

echo "==> install on ${HOST} (preserve fnOS folder grants)"
# shellcheck disable=SC2087
ssh "${HOST}" "VERSION='${VERSION}' REMOTE_FPK='${REMOTE_FPK}' REMOTE_TPK='${REMOTE_TPK}' bash -s" <<'EOS'
set -euo pipefail

PSQL=(sudo -n -u postgres psql -d appcenter -v ON_ERROR_STOP=1)
backup="$(mktemp)"
trap 'rm -f "$backup"' EXIT

echo "--> snapshot authorization paths"
"${PSQL[@]}" -Atc "
SELECT 'shared|' || p.path
FROM app_authorization_path p
JOIN app a ON a.id = p.app_id
WHERE a.app_name = 'roundpen'
UNION ALL
SELECT 'user|' || u.uid::text || '|' || u.path || '|' || u.perm::text || '|' || COALESCE(u.trimacl_perm::text, '')
FROM app_user_authorization_path u
JOIN app a ON a.id = u.app_id
WHERE a.app_name = 'roundpen';
" >"$backup"
echo "    saved $(grep -c . "$backup" || true) path(s)"

echo "--> install-fpk (unpack)"
sudo -n appcenter-cli install-fpk "$REMOTE_FPK"

echo "--> install-local (stop/uninstall/install — appdata kept)"
sudo -n bash -c "
  mkdir -p /vol1/tmp-appcenter
  chmod 1777 /vol1/tmp-appcenter
  chmod -R a+rX '$REMOTE_TPK'
  find '$REMOTE_TPK/cmd' -type f -exec chmod 0755 {} +
  TMPDIR=/vol1/tmp-appcenter TEMP=/vol1/tmp-appcenter TMP=/vol1/tmp-appcenter \
    appcenter-cli install-local -d '$REMOTE_TPK'
"

echo "--> restore authorization paths onto new app_id"
app_id="$("${PSQL[@]}" -Atc "SELECT id FROM app WHERE app_name='roundpen'")"
[[ -n "$app_id" ]] || { echo "roundpen app row missing after install" >&2; exit 1; }

while IFS= read -r line || [[ -n "$line" ]]; do
  [[ -z "$line" ]] && continue
  kind="${line%%|*}"
  rest="${line#*|}"
  case "$kind" in
  shared)
    path="$rest"
    "${PSQL[@]}" -v path="$path" -c "
      INSERT INTO app_authorization_path (app_id, path, created_at, updated_at)
      SELECT ${app_id}, :'path', now(), now()
      WHERE NOT EXISTS (
        SELECT 1 FROM app_authorization_path
        WHERE app_id = ${app_id} AND path = :'path'
      );
    " >/dev/null
    echo "    shared: $path"
    ;;
  user)
    IFS='|' read -r uid path perm trimacl <<<"$rest"
    case "${uid:-}" in ''|*[!0-9]*) echo "    bad uid in: $line" >&2; continue ;; esac
    case "${perm:-0}" in ''|*[!0-9]*) perm=0 ;; esac
    trimacl_sql="NULL"
    case "${trimacl:-}" in '' ) ;; *[!0-9]* ) ;; *) trimacl_sql="$trimacl" ;; esac
    "${PSQL[@]}" -v path="$path" -c "
      INSERT INTO app_user_authorization_path
        (app_id, uid, path, trimacl_perm, created_at, updated_at, perm)
      SELECT ${app_id}, ${uid}, :'path', ${trimacl_sql}, now(), now(), ${perm}
      WHERE NOT EXISTS (
        SELECT 1 FROM app_user_authorization_path
        WHERE app_id = ${app_id} AND uid = ${uid} AND path = :'path'
      );
    " >/dev/null
    echo "    user uid=$uid: $path"
    ;;
  *)
    echo "    skip unknown backup line: $line" >&2
    ;;
  esac
done <"$backup"

if [[ -s "$backup" ]]; then
  echo "--> restart so TRIM_DATA_ACCESSIBLE_PATHS is reinjected"
  sudo -n appcenter-cli stop roundpen
  sudo -n appcenter-cli start roundpen
fi

echo "--> verify"
sudo -n appcenter-cli list | grep -E 'APP NAME|roundpen' || true
curl -fsS http://127.0.0.1:9527/health; echo
test -f /vol1/@appdata/roundpen/data/secret.key
pid="$(pgrep -n roundpend)"
acc="$(sudo -n cat /proc/$pid/environ | tr '\0' '\n' | sed -n 's/^TRIM_DATA_ACCESSIBLE_PATHS=//p')"
echo "TRIM_DATA_ACCESSIBLE_PATHS=${acc}"
"${PSQL[@]}" -Atc "
SELECT 'db-shared|' || p.path FROM app_authorization_path p
JOIN app a ON a.id=p.app_id WHERE a.app_name='roundpen'
UNION ALL
SELECT 'db-user|' || u.uid::text || '|' || u.path FROM app_user_authorization_path u
JOIN app a ON a.id=u.app_id WHERE a.app_name='roundpen';
"
EOS

echo "==> done ${VERSION} on ${HOST}"
