# Nginx admin login rate limiting

API contains no process-local login limiter. Validate and activate ingress throttling before deploying an API build without that limiter. Repository Compose does not install or configure Nginx.

Define bounded per-client state in Nginx `http` context:

```nginx
limit_req_zone $binary_remote_addr zone=messeances_admin_login:1m rate=1r/m;
```

Use an exact login location in the HTTPS server:

```nginx
location = /api/v1/admin/login {
    limit_req zone=messeances_admin_login burst=4 nodelay;
    limit_req_status 429;

    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Keep API port bound to loopback and unreachable from public networks. This limit counts requests, not failed passwords. Permitted requests retain API generic `401` responses; excess requests receive ingress `429` responses.

Never key limits from arbitrary `X-Forwarded-For`. If another trusted proxy precedes Nginx, restore real client addresses only with explicitly enumerated `set_real_ip_from` CIDRs and matching `real_ip_header` configuration. Validate resulting client keys, run `nginx -t`, reload safely, and test exact-path behavior before API rollout.
