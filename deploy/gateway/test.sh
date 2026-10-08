#!/usr/bin/env bash
# 隔离的部署验收：只使用假后端和临时证书，不连接业务依赖。
set -euo pipefail
cd "$(dirname "$0")/../.."
repository=$(pwd)
temporary=$(mktemp -d)
project="eagle-gateway-test-$$"
network="${project}_default"
containers=()
export EAGLE_TLS_DIR="$temporary/tls"
export EAGLE_BIND_ADDRESS=127.0.0.1 EAGLE_HTTP_PORT=0 EAGLE_HTTPS_PORT=0
export EAGLE_API_HOST=api.example.com EAGLE_CORS_ORIGIN=https://app.example.com
compose=(docker compose --project-name "$project" --env-file deploy/compose/.env.example -f deploy/compose/compose.yml)

cleanup() {
  if [ "${#containers[@]}" -gt 0 ]; then docker rm -f "${containers[@]}" >/dev/null 2>&1 || true; fi
  "${compose[@]}" down --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$temporary"
}
trap cleanup EXIT

mkdir "$temporary/tls"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj /CN=api.example.com -addext subjectAltName=DNS:api.example.com \
  -keyout "$temporary/tls/privkey.pem" -out "$temporary/tls/fullchain.pem" >/dev/null 2>&1
# Linux bind mount 保留 UID；使用与生产一致的 root:root/0600，而不扩大私钥读取权限。
containers+=("${project}-certificate")
docker run --rm --name "${project}-certificate" --network none \
  -v "$temporary/tls:/certificates" nginx:1.30.5-alpine \
  sh -c 'chown 0:0 /certificates/privkey.pem && chmod 600 /certificates/privkey.pem' >/dev/null

"${compose[@]}" config --quiet
"${compose[@]}" up -d --no-deps gateway
https_port=$("${compose[@]}" port gateway 443 | awk -F: '{print $NF}')
http_port=$("${compose[@]}" port gateway 80 | awk -F: '{print $NF}')

backend() {
  local service=$1 version=$2 container="${project}-$1-$2"
  cat >"$temporary/$service-$version.conf" <<EOF
events {}
http {
  access_log /dev/stdout;
  server {
    listen 8000;
    location / {
      return 200 "$service-$version|\$request_uri|\$http_authorization|\$request_method";
    }
  }
}
EOF
  containers+=("$container")
  docker run -d --name "$container" --network "$network" --network-alias "$service" \
    -v "$temporary/$service-$version.conf:/etc/nginx/nginx.conf:ro" nginx:1.30.5-alpine >/dev/null
}
backend admin v1
backend product v1
backend order v1

request() {
  curl --noproxy '*' --silent --show-error --fail --max-time 5 --cacert "$temporary/tls/fullchain.pem" \
    --resolve "api.example.com:$https_port:127.0.0.1" "https://api.example.com:$https_port$1" "${@:2}"
}
expect() {
  if [ "$1" != "$2" ]; then printf 'expected: %s\nactual: %s\n' "$2" "$1" >&2; exit 1; fi
}
ready=false
for attempt in $(seq 1 30); do
  if request /v1/products >"$temporary/response" 2>/dev/null; then ready=true; break; fi
  sleep 1
done
[ "$ready" = true ] || { request /edgez || true; "${compose[@]}" logs gateway; exit 1; }
expect "$(request '/v1/products?limit=2')" 'product-v1|/v1/products?limit=2||GET'
expect "$(request /v1/orders -X POST -H 'Authorization: Bearer fixture-token' -d '{}')" \
  'order-v1|/v1/orders|Bearer fixture-token|POST'
expect "$(request /v1/system/files)" 'admin-v1|/v1/system/files||GET'
expect "$(request /v1/system/notifications)" 'admin-v1|/v1/system/notifications||GET'
expect "$(request /v1/system/dictionaries)" 'admin-v1|/v1/system/dictionaries||GET'
expect "$(request /edgez)" 'ok'
request /v1/products -H 'Origin: https://app.example.com' -D "$temporary/headers" >/dev/null
grep -qi '^Access-Control-Allow-Origin: https://app.example.com' "$temporary/headers"
request /v1/products -H 'Origin: https://untrusted.example.com' -D "$temporary/headers" >/dev/null
if grep -qi '^Access-Control-Allow-Origin:' "$temporary/headers"; then exit 1; fi
expect "$(request /v1/orders -X OPTIONS -H 'Origin: https://app.example.com' -o /dev/null -w '%{http_code}')" 204
curl --noproxy '*' --silent --show-error --max-time 5 -D "$temporary/headers" -o /dev/null \
  "http://127.0.0.1:$http_port/v1/orders?limit=2"
grep -qi '^Location: https://api.example.com/v1/orders?limit=2' "$temporary/headers"
"${compose[@]}" exec -T gateway nginx -t
"${compose[@]}" exec -T gateway wget -q -O /dev/null http://127.0.0.1:8080/edgez

# 开发入口也使用同一组路由和动态 upstream。
containers+=("${project}-dev")
docker run -d --name "${project}-dev" --network "$network" -p 127.0.0.1::8000 \
  -v "$repository/deploy/gateway/nginx.conf:/etc/nginx/nginx.conf:ro" \
  -v "$repository/deploy/gateway/upstreams.conf:/etc/nginx/upstreams.conf:ro" \
  -v "$repository/deploy/gateway/routes.conf:/etc/nginx/routes.conf:ro" \
  -v "$repository/deploy/gateway/proxy.conf:/etc/nginx/proxy.conf:ro" nginx:1.30.5-alpine >/dev/null
dev_port=$(docker port "${project}-dev" 8000/tcp | awk -F: '{print $NF}')
for attempt in $(seq 1 20); do
  if curl --noproxy '*' --silent --fail "http://127.0.0.1:$dev_port/v1/products" >"$temporary/response"; then break; fi
  sleep 1
done
expect "$(cat "$temporary/response")" 'product-v1|/v1/products||GET'

# 占住旧 IP，确保重建确实改变 IP；入口必须无需 reload 就能恢复。
old_ip=$(docker inspect "${project}-product-v1" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
docker rm -f "${project}-product-v1" >/dev/null
containers+=("${project}-old-ip")
docker run -d --name "${project}-old-ip" --network "$network" --ip "$old_ip" \
  nginx:1.30.5-alpine sleep 120 >/dev/null
backend product v2
new_ip=$(docker inspect "${project}-product-v2" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
[ "$old_ip" != "$new_ip" ]
recovered=false
for attempt in $(seq 1 25); do
  production=$(request /v1/products 2>/dev/null || true)
  development=$(curl --noproxy '*' --silent --fail --max-time 3 "http://127.0.0.1:$dev_port/v1/products" || true)
  if [ "$production" = 'product-v2|/v1/products||GET' ] && [ "$development" = "$production" ]; then
    recovered=true
    break
  fi
  sleep 1
done
[ "$recovered" = true ] || { "${compose[@]}" logs gateway; exit 1; }
printf '%s\n' 'PASS: Compose gateway HTTPS, routes, CORS, headers and recovery after backend IP change'
