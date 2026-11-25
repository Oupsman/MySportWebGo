#!/usr/bin/env bash
# Audits a MySportWeb installation from the outside, through its public URL: TLS, security headers, CORS and
# CSRF, what must not be reachable (metrics, database, files), the authentication, and with a token what the
# instance reports about itself.
#
#   scripts/audit.sh https://sport.example.com                     # anonymous checks only
#   MSW_AUDIT_TOKEN=msw_... scripts/audit.sh https://sport.example.com
#
# The token: a device token (msw_..., Profile > Device tokens), or the session token of an admin logged in with
# their second factor (the __Host-msw_session cookie of the browser) for the admin status report. Passed through
# the environment, not the command line: the arguments are visible to every user of the machine (ps).
#
# Read only: nothing is created nor changed. The only POSTs go to /api/v1/user/logout without credentials (the
# CSRF and body size checks), which does nothing then. No login is attempted: the rate limit isn't touched.
#
# Needs curl, jq, openssl. Exit status: 0 all passed, 1 a warning, 2 a failure.
set -uo pipefail

usage() { echo "usage: MSW_AUDIT_TOKEN=<token> $0 <public URL> (the token is optional)" >&2; exit 64; }
[ $# -eq 1 ] || usage
for tool in curl jq openssl; do
  command -v "$tool" > /dev/null || { echo "$tool is missing" >&2; exit 64; }
done

BASE="${1%/}"
TOKEN="${MSW_AUDIT_TOKEN:-}"
[[ "$BASE" =~ ^(https?)://([^/:]+)(:([0-9]+))?$ ]] || { echo "the URL must be scheme://host[:port], no path" >&2; exit 64; }
SCHEME="${BASH_REMATCH[1]}"
HOST="${BASH_REMATCH[2]}"
PORT="${BASH_REMATCH[4]:-$([ "$SCHEME" = https ] && echo 443 || echo 80)}"
EVIL="https://audit-evil.example"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [ -t 1 ]; then
  C_OK=$'\e[32m'; C_WARN=$'\e[33m'; C_FAIL=$'\e[31m'; C_INFO=$'\e[36m'; C_BOLD=$'\e[1m'; C_END=$'\e[0m'
else
  C_OK=; C_WARN=; C_FAIL=; C_INFO=; C_BOLD=; C_END=
fi
PASSES=0; WARNINGS=0; FAILURES=0
pass() { PASSES=$((PASSES + 1)); echo "  ${C_OK}PASS${C_END} $*"; }
warn() { WARNINGS=$((WARNINGS + 1)); echo "  ${C_WARN}WARN${C_END} $*"; }
fail() { FAILURES=$((FAILURES + 1)); echo "  ${C_FAIL}FAIL${C_END} $*"; }
info() { echo "  ${C_INFO}INFO${C_END} $*"; }
section() { echo; echo "${C_BOLD}== $* ==${C_END}"; }

# req METHOD PATH [curl args...]: the status in $STATUS, the headers in $WORK/h, the body in $WORK/b
req() {
  local method="$1" path="$2"; shift 2
  STATUS="$(curl -sS -o "$WORK/b" -D "$WORK/h" -w '%{http_code}' --max-time 20 -X "$method" "$@" "$BASE$path" 2>"$WORK/e")" \
    || STATUS="000"
}
# areq: req with the token, never sent over plain HTTP
areq() {
  local method="$1" path="$2"; shift 2
  req "$method" "$path" -H "Authorization: Bearer $TOKEN" "$@"
}
header() { grep -i "^$1:" "$WORK/h" | tail -1 | cut -d: -f2- | tr -d '\r' | sed 's/^ *//'; }
body() { cat "$WORK/b"; }
json() { jq -r "$1" "$WORK/b" 2>/dev/null; }

# tcp_open HOST PORT: whether a TCP connection succeeds within 3 s
tcp_open() { timeout 3 bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; }

echo "${C_BOLD}MySportWeb audit of $BASE${C_END} ($(date '+%F %T'))"

# --------------------------------------------------------------------------------------------------------------
section "Reachability"
req GET /api/v1/healthcheck
if [ "$STATUS" = 000 ]; then
  fail "the instance doesn't answer: $(cat "$WORK/e")"
  echo; echo "${C_FAIL}aborted${C_END}"; exit 2
fi
if [ "$STATUS" = 200 ] && [ "$(json .status)" = OK ]; then
  VERSION="$(json .version)"
  pass "healthcheck OK, version $VERSION, database reachable"
else
  fail "healthcheck answered $STATUS: $(body | head -c 200)"
  VERSION=""
fi
TIME="$(curl -sS -o /dev/null -w '%{time_total}' --max-time 20 "$BASE/api/v1/healthcheck" 2>/dev/null || echo 99)"
if awk "BEGIN{exit !($TIME < 1)}"; then pass "healthcheck in ${TIME}s"; else warn "healthcheck slow: ${TIME}s"; fi

# the latest release, when run from a clone of the repository
if [ -n "$VERSION" ] && git -C "$(dirname "$0")" rev-parse --git-dir > /dev/null 2>&1; then
  LATEST="$(git -C "$(dirname "$0")" tag --list 'v[0-9]*.[0-9]*.[0-9]*' '[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -1)"
  if [ -n "$LATEST" ]; then
    if [ "${VERSION#v}" = "${LATEST#v}" ]; then
      pass "the latest release ($LATEST)"
    else
      warn "version $VERSION deployed, the latest release is $LATEST"
    fi
  fi
fi

req GET /
if [ "$STATUS" = 200 ] && grep -qi '<html' "$WORK/b"; then pass "the frontend answers"; else fail "the frontend answered $STATUS"; fi

# --------------------------------------------------------------------------------------------------------------
section "TLS"
if [ "$SCHEME" != https ]; then
  fail "served over plain HTTP: passwords, session cookies and tokens travel in clear (the session cookie isn't even Secure)"
  if [ -n "$TOKEN" ]; then
    warn "the token is not sent over plain HTTP: the authenticated checks are skipped"
    TOKEN=""
  fi
else
  CERT="$(echo | openssl s_client -connect "$HOST:$PORT" -servername "$HOST" 2>/dev/null | openssl x509 2>/dev/null)"
  if [ -z "$CERT" ]; then
    fail "no certificate could be read on $HOST:$PORT"
  else
    if curl -sS -o /dev/null --max-time 10 "$BASE/" 2>/dev/null; then
      pass "certificate valid for $HOST (chain and name checked by curl)"
    else
      fail "certificate refused by curl: $(curl -sS -o /dev/null --max-time 10 "$BASE/" 2>&1 | head -1)"
    fi
    END="$(echo "$CERT" | openssl x509 -noout -enddate | cut -d= -f2)"
    DAYS=$(( ($(date -d "$END" +%s) - $(date +%s)) / 86400 ))
    if [ "$DAYS" -lt 7 ]; then fail "certificate expires in $DAYS days ($END)"
    elif [ "$DAYS" -lt 21 ]; then warn "certificate expires in $DAYS days ($END): is the renewal running?"
    else pass "certificate expires in $DAYS days"; fi
    info "issuer: $(echo "$CERT" | openssl x509 -noout -issuer | sed 's/^issuer= *//')"
  fi
  # the local openssl may not offer the old versions at all (security level): only an accepted one tells
  tls_ok() { echo | openssl s_client -connect "$HOST:$PORT" -servername "$HOST" "-$1" > /dev/null 2>&1; }
  tls_ok tls1_3 && pass "TLS 1.3 offered" || warn "TLS 1.3 not offered"
  tls_ok tls1_2 && info "TLS 1.2 offered"
  OLD=""
  tls_ok tls1 && OLD="1.0"
  tls_ok tls1_1 && OLD="${OLD:+$OLD and }1.1"
  [ -n "$OLD" ] && fail "TLS $OLD accepted" || pass "TLS 1.0 and 1.1 not accepted"

  # plain HTTP on port 80: a redirect to HTTPS, never the app
  HTTP_STATUS="$(curl -sS -o /dev/null -w '%{http_code} %{redirect_url}' --max-time 5 "http://$HOST/" 2>/dev/null || echo 000)"
  case "$HTTP_STATUS" in
    000*) pass "port 80 closed" ;;
    30[1278]\ https://*) pass "HTTP redirects to HTTPS (${HTTP_STATUS%% *})" ;;
    *) fail "HTTP on port 80 answers ${HTTP_STATUS} instead of redirecting to HTTPS" ;;
  esac
fi

# --------------------------------------------------------------------------------------------------------------
section "Security headers"
# the frontend's nginx sets them on the pages and on the API: a reverse proxy in front may drop or override them
check_headers() {
  local where="$1"
  local csp; csp="$(header Content-Security-Policy)"
  if [ -n "$csp" ]; then
    if [[ "$csp" == *"frame-ancestors 'none'"* && "$csp" == *"script-src 'self'"* && "$csp" != *"script-src 'self' 'unsafe"* ]]; then
      pass "$where: Content-Security-Policy enforced"
    else
      warn "$where: Content-Security-Policy weaker than the one of the frontend: $csp"
    fi
  elif [ -n "$(header Content-Security-Policy-Report-Only)" ]; then
    warn "$where: CSP only reported (MSW_CSP_HEADER=Content-Security-Policy-Report-Only): nothing is blocked"
  else
    fail "$where: no Content-Security-Policy"
  fi
  local hsts; hsts="$(header Strict-Transport-Security)"
  if [ "$SCHEME" = https ]; then
    local age; age="$(sed -n 's/.*max-age=\([0-9]*\).*/\1/p' <<< "$hsts")"
    if [ -z "$hsts" ]; then fail "$where: no Strict-Transport-Security"
    elif [ "${age:-0}" -lt 15552000 ]; then warn "$where: Strict-Transport-Security max-age $age, under 180 days"
    else pass "$where: Strict-Transport-Security $hsts"; fi
  fi
  local name expected value
  for pair in "X-Content-Type-Options=nosniff" "X-Frame-Options=DENY" "Referrer-Policy=strict-origin-when-cross-origin"; do
    name="${pair%%=*}"; expected="${pair#*=}"; value="$(header "$name")"
    if [ "${value,,}" = "${expected,,}" ]; then pass "$where: $name $value"
    elif [ -z "$value" ]; then fail "$where: no $name"
    elif [ "$name" = X-Frame-Options ] && [[ "$csp" == *"frame-ancestors 'none'"* ]]; then
      # the browsers follow frame-ancestors of the CSP and ignore X-Frame-Options when both are there
      info "$where: X-Frame-Options is '$value', not the DENY of the frontend: a proxy in front rewrites it (harmless, the CSP frame-ancestors 'none' wins)"
    else warn "$where: $name is '$value', expected '$expected' (a proxy in front rewrites it?)"; fi
  done
  [ -n "$(header Permissions-Policy)" ] && pass "$where: Permissions-Policy" || warn "$where: no Permissions-Policy"
}
req GET /
check_headers "pages"
SERVER="$(header Server)"
if [[ "$SERVER" =~ [0-9]+\.[0-9]+ ]]; then
  warn "the Server header tells the version: '$SERVER' (server_tokens off in nginx)"
else
  pass "Server header without version${SERVER:+ ('$SERVER')}"
fi
[ -n "$(header X-Powered-By)" ] && warn "X-Powered-By: $(header X-Powered-By)"
[ "$(header Cache-Control)" = no-cache ] && pass "index.html is revalidated (Cache-Control no-cache)" \
  || warn "index.html Cache-Control is '$(header Cache-Control)': a release may not reach the browsers"

req GET /api/v1/healthcheck
check_headers "API"

# --------------------------------------------------------------------------------------------------------------
section "Instance configuration"
req GET /api/v1/instance
if [ "$STATUS" = 200 ]; then
  REGISTRATION="$(json .registration)"
  case "$REGISTRATION" in
    closed) pass "registration closed" ;;
    invite) pass "registration on invitation" ;;
    open) warn "registration open: anyone reaching the instance can create an account and use its storage" ;;
    *) warn "registration '$REGISTRATION'" ;;
  esac
  if [ "$(json .setup_required)" = true ]; then
    fail "no admin yet: the instance is unclaimed (create the admin with the setup code of the backend logs)"
  else
    pass "an admin exists"
  fi
  if [ "$(json .email)" = true ]; then pass "emails configured"
  else warn "no email: no forgotten password, no address confirmation, no notification"; fi
else
  fail "/api/v1/instance answered $STATUS"
fi

# --------------------------------------------------------------------------------------------------------------
section "CORS and CSRF"
req GET /api/v1/healthcheck -H "Origin: $EVIL"
ACAO="$(header Access-Control-Allow-Origin)"
if [ -z "$ACAO" ]; then pass "no CORS for another origin"
else fail "CORS allows another origin: Access-Control-Allow-Origin: $ACAO $(header Access-Control-Allow-Credentials)"; fi

req OPTIONS /api/v1/user -H "Origin: $EVIL" -H "Access-Control-Request-Method: POST"
ACAO="$(header Access-Control-Allow-Origin)"
if [ -z "$ACAO" ]; then pass "preflight from another origin refused ($STATUS)"
else fail "preflight from another origin allowed: $ACAO"; fi

# DOMAIN is the only trusted origin of the CSRF protection: a POST announced cross-site but from it passes.
# (No CORS headers to look at: the cors middleware skips the requests whose Origin is the Host, same origin.)
req POST /api/v1/user/logout -H "Origin: $BASE" -H "Sec-Fetch-Site: cross-site"
if [ "$STATUS" = 200 ]; then pass "DOMAIN matches the public URL"
else fail "a POST from $BASE is refused ($STATUS): MSW_DOMAIN isn't exactly this URL (scheme, host, port, no slash)"; fi

# a page of another site posting with the cookie of the user: refused before any handler
req POST /api/v1/user/logout -H "Origin: $EVIL" -H "Sec-Fetch-Site: cross-site"
if [ "$STATUS" = 403 ]; then pass "cross-site POST refused (CSRF protection)"
else fail "cross-site POST answered $STATUS instead of 403"; fi

# --------------------------------------------------------------------------------------------------------------
section "Authentication"
req GET /api/v1/user
[ "$STATUS" = 401 ] && pass "the API needs credentials (401 without)" || fail "GET /api/v1/user without credentials: $STATUS"
req GET /api/v1/user -H "Authorization: Bearer msw_audit_invalid_token"
[ "$STATUS" = 401 ] && pass "an unknown device token is refused" || fail "an unknown device token: $STATUS"
# a JWT signed with "none": a library accepting it lets anyone in
NONE_JWT="eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJleHAiOjQxMDI0NDQ4MDB9."
req GET /api/v1/user -H "Authorization: Bearer $NONE_JWT"
[ "$STATUS" = 401 ] && pass "an unsigned JWT (alg none) is refused" || fail "an unsigned JWT: $STATUS"
req GET /api/v1/admin/status
[ "$STATUS" = 401 ] && pass "the admin API needs credentials" || fail "GET /api/v1/admin/status without credentials: $STATUS"

# --------------------------------------------------------------------------------------------------------------
section "Exposure"
# the SPA answers index.html for any path: a file is exposed only when its content is there
exposed() {
  local path="$1" signature="$2"
  req GET "$path"
  if [ "$STATUS" = 200 ] && grep -q -- "$signature" "$WORK/b"; then fail "$path is served"
  else pass "$path not served"; fi
}
exposed /.env "SECRET_KEY\|DBPASSWORD"
exposed /.git/config "\[core\]"
exposed /docker-compose.yaml "services:"
exposed /metrics "go_goroutines\|# HELP"
exposed /api/metrics "go_goroutines\|# HELP"
exposed /api/v1/../../../../etc/passwd "root:"
req GET "/MEDIA/..%2f..%2f..%2fetc%2fpasswd" --path-as-is
{ [ "$STATUS" = 200 ] && grep -q "root:" "$WORK/b"; } && fail "/MEDIA path traversal" || pass "/MEDIA path traversal refused ($STATUS)"
req GET /MEDIA/00000000-0000-0000-0000-000000000000/activity.fit
[ "$STATUS" = 404 ] && pass "/MEDIA hides the files of others (404)" || warn "/MEDIA of an unknown user: $STATUS"

req GET /api/v1/audit-no-such-route
if grep -qi "goroutine\|panic\|\.go:[0-9]" "$WORK/b"; then fail "an unknown route leaks internals"
else pass "an unknown route answers $STATUS without internals"; fi

# the body limits of the backend (1 MB for JSON); announced too large: refused before reading
STATUS="$(head -c $((2 << 20)) /dev/zero | curl -sS -o /dev/null -w '%{http_code}' --max-time 20 -X POST \
  -H 'Content-Type: application/json' --data-binary @- "$BASE/api/v1/user/logout" 2>/dev/null || echo 000)"
[ "$STATUS" = 413 ] && pass "a 2 MB JSON body is refused (413)" || warn "a 2 MB JSON body: $STATUS instead of 413"

# the ports of the docker host that must not be public: the database, the metrics (127.0.0.1 by default)
if tcp_open "$HOST" 5432; then fail "PostgreSQL port 5432 open on $HOST"; else pass "port 5432 closed"; fi
if tcp_open "$HOST" 9090; then
  M="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "http://$HOST:9090/metrics" 2>/dev/null || echo 000)"
  case "$M" in
    200) fail "metrics public without token on http://$HOST:9090/metrics (MSW_METRICS_TOKEN)" ;;
    401) warn "metrics published on $HOST:9090, behind a token but over plain HTTP" ;;
    *) warn "port 9090 open on $HOST (answered $M)" ;;
  esac
else
  pass "port 9090 (metrics) closed"
fi
if [ "$PORT" != 8080 ] && tcp_open "$HOST" 8080; then
  M="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "http://$HOST:8080/api/v1/healthcheck" 2>/dev/null || echo 000)"
  [ "$M" = 200 ] && warn "the app is also reachable over plain HTTP on $HOST:8080, around the TLS proxy (MSW_HTTP_PORT=127.0.0.1:8080?)" \
    || info "port 8080 open on $HOST (answered $M)"
fi

# --------------------------------------------------------------------------------------------------------------
if [ -n "$TOKEN" ]; then
  section "Authenticated"
  if [[ "$TOKEN" == msw_* ]]; then
    areq GET /api/v1/token
    if [ "$STATUS" != 200 ]; then
      fail "the device token is refused ($STATUS): $(body | head -c 200)"
    else
      pass "device token '$(json .device_token.name)' valid"
      PERMS="$(json '.device_token.permissions | join(",")')"
      info "permissions: $PERMS"
      EXP="$(json .device_token.expires_at)"
      DAYS=$(( ($(date -d "$EXP" +%s) - $(date +%s)) / 86400 ))
      if [ "$DAYS" -lt 14 ]; then warn "the token expires in $DAYS days"
      elif [ "$DAYS" -gt 400 ]; then warn "the token expires in $DAYS days: long-lived"
      else pass "the token expires in $DAYS days"; fi
      [[ "$PERMS" == *:delete* ]] && warn "the token can delete: a leaked token loses data, keep the deletes for the tokens that need them"

      # what a device token must never reach
      areq GET /api/v1/user/tokens
      [ "$STATUS" = 403 ] && pass "a device token can't manage the tokens" || fail "a device token on /user/tokens: $STATUS"
      areq GET /api/v1/user/export
      [ "$STATUS" = 403 ] && pass "a device token can't export the account" || fail "a device token on /user/export: $STATUS"
      areq GET /api/v1/admin/status
      [ "$STATUS" = 403 ] && pass "a device token can't reach the admin API" || fail "a device token on /admin/status: $STATUS"

      if [[ "$PERMS" == *activities:read* ]]; then
        T="$(curl -sS -o "$WORK/b" -w '%{http_code} %{time_total}' --max-time 30 -H "Authorization: Bearer $TOKEN" \
          "$BASE/api/v1/activity/list/0/20" 2>/dev/null || echo "000 0")"
        if [ "${T%% *}" = 200 ]; then pass "20 activities listed in ${T#* }s"
        else info "activity list: ${T%% *} (the token may lack the read permission)"; fi
      fi
    fi
  else
    areq GET /api/v1/user
    if [ "$STATUS" != 200 ]; then
      fail "the session token is refused ($STATUS): $(body | head -c 200)"
    else
      ROLE="$(json .role)"
      pass "session of $(json .username) ($ROLE)"
      areq GET /api/v1/user/2fa
      if [ "$(json .enabled)" = true ]; then pass "two-factor authentication on ($(json .recovery_codes_left) recovery codes left)"
        [ "$(json .recovery_codes_left)" -lt 3 ] 2>/dev/null && warn "only $(json .recovery_codes_left) recovery codes left"
      elif [ "$(json .required)" = true ]; then fail "an admin without two-factor authentication"
      else warn "two-factor authentication off"; fi

      if [ "$ROLE" = admin ]; then
        areq GET /api/v1/admin/status
        if [ "$STATUS" = 200 ]; then
          cp "$WORK/b" "$WORK/status.json"
          info "version $(json .version), schema $(json .schema), up since $(json .started_at)"
          info "$(json .users) users, $(json .activities) activities"
          info "memory $(json '(.memory.resident/1048576|floor|tostring) + " MB of " + (if .memory.limit > 0 then (.memory.limit/1048576|floor|tostring) + " MB" else "no limit" end)')"
          info "database connections $(json '"\(.database.in_use) in use, max \(.database.max)"')"
          DISK="$(json 'if .disk then ((.disk.free*100/.disk.total)|floor) else empty end')"
          if [ -n "$DISK" ]; then
            if [ "$DISK" -lt 10 ]; then fail "media disk: $DISK% free"
            elif [ "$DISK" -lt 20 ]; then warn "media disk: $DISK% free"
            else pass "media disk: $DISK% free ($(json '.disk.free/1073741824|floor') GB)"; fi
          fi
          if [ "$(json .backups.configured)" != true ]; then warn "backups: not configured for the backend"
          elif [ "$(json .backups.readable)" != true ]; then warn "backups: the backend can't read them (MSW_BACKUP_OWNER)"
          elif [ "$(json .backups.last_success)" = null ]; then fail "backups: none succeeded"
          else
            AGE=$(( ($(date +%s) - $(date -d "$(json .backups.last_success)" +%s)) / 3600 ))
            [ "$AGE" -le 26 ] && pass "last backup ${AGE}h ago" || fail "last backup ${AGE}h ago"
          fi
          info "last day: $(json '"\(.last_day.requests|floor) requests, \(.last_day.errors|floor) 5xx, logins \(.last_day.logins // {} | tojson)"')"
          # the server's own checks
          while IFS=$'\t' read -r level name message; do
            case "$level" in
              ok) pass "status: $name${message:+: $message}" ;;
              warning) warn "status: $name: $message" ;;
              critical) fail "status: $name: $message" ;;
              *) info "status: $name ($level): $message" ;;
            esac
          done < <(jq -r '.checks[]? | [.level, .name, .message] | @tsv' "$WORK/status.json")
        elif [ "$(json .code)" = mfa_required ]; then
          warn "admin status not read: this session wasn't opened with the second factor"
        else
          fail "admin status: $STATUS $(body | head -c 200)"
        fi
      fi
    fi
  fi
else
  section "Authenticated"
  info "skipped: set MSW_AUDIT_TOKEN (a device token, or an admin session token for the status report)"
fi

# --------------------------------------------------------------------------------------------------------------
echo
echo "${C_BOLD}$PASSES passed, ${C_WARN}$WARNINGS warnings${C_END}${C_BOLD}, ${C_FAIL}$FAILURES failures${C_END}"
[ "$FAILURES" -gt 0 ] && exit 2
[ "$WARNINGS" -gt 0 ] && exit 1
exit 0
