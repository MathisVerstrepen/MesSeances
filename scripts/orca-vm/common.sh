#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
local_dir="$script_dir/.local"
state_file="${ORCA_DOCKER_STATE:-$local_dir/state.json}"
mkdir -p "$local_dir"
chmod 700 "$local_dir"
if [ ! -f "$state_file" ]; then
  cp "$script_dir/docker-state.json" "$state_file"
fi
value() {
  node -e 'const fs=require("fs"); const [p,k,n]=process.argv.slice(1); const d=JSON.parse(fs.readFileSync(p)); process.stdout.write(String(process.env[n] ?? d[k] ?? ""))' "$state_file" "$1" "$2"
}
merge_state() {
  node -e 'const fs=require("fs"); const [p,s]=process.argv.slice(1); const d={...JSON.parse(fs.readFileSync(p)),...JSON.parse(s)}; fs.writeFileSync(p+".tmp",JSON.stringify(d,null,2)+"\n",{mode:0o600}); fs.renameSync(p+".tmp",p); console.log(JSON.stringify(d))' "$state_file" "$1"
}
ensure_key() {
  key_file="$local_dir/id_ed25519"
  if [ ! -f "$key_file" ]; then
    ssh-keygen -q -t ed25519 -N '' -C messeances-orca -f "$key_file" >&2
  fi
  public_key="$(<"$key_file.pub")"
}
owned() {
  [ "$(docker inspect --format '{{index .Config.Labels "fr.messeances.orca"}}' "$1")" = local-docker ] || {
    printf 'Refusing resource without MesSeances recipe label: %s\n' "$1" >&2; return 1;
  }
}
payload_values() {
  payload="$(cat)"
  resource_id="$(node -e 'const d=JSON.parse(process.argv[1]); process.stdout.write(d.recipeResult?.userData?.resourceId ?? "")' "$payload")"
  [[ "$resource_id" =~ ^[a-f0-9]{64}$ ]] || { printf 'Missing or invalid Docker resource id\n' >&2; exit 1; }
  owned "$resource_id"
  instance="$(docker inspect --format '{{index .Config.Labels "fr.messeances.orca.instance"}}' "$resource_id")"
  [[ "$instance" =~ ^messeances-orca-[a-z0-9-]+$ ]] || { printf 'Invalid instance label\n' >&2; exit 1; }
}
record_host_key() {
  ssh_port="$(docker port "$resource_id" 22/tcp)"
  ssh_port="${ssh_port##*:}"
  host_key=""
  for attempt in {1..60}; do
    if host_key="$(docker exec "$resource_id" cat /etc/ssh/ssh_host_ed25519_key.pub 2>/dev/null)" && [ -n "$host_key" ]; then break; fi
    sleep 1
  done
  [ -n "$host_key" ] || { docker logs "$resource_id" >&2; return 1; }
  # Trust only the key read from this locally owned container, never ssh-keyscan.
  node "$script_dir/known-hosts.mjs" add "$ssh_port" "$host_key"
}
result() {
  ensure_key
  record_host_key
  web_port="$(docker port "$resource_id" 3000/tcp)"; web_port="${web_port##*:}"
  api_port="$(docker port "$resource_id" 8080/tcp)"; api_port="${api_port##*:}"
  node -e 'const port=process.argv[1];const env={PATH:"/home/mathis/.local/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",DATABASE_URL:"postgres://movieflow:movieflow@postgres:5432/movieflow?sslmode=disable",TEST_DATABASE_URL:"postgres://movieflow:movieflow@postgres:5432/movieflow?sslmode=disable",HOST:"0.0.0.0",NUXT_API_BASE:"http://localhost:8080",NUXT_PUBLIC_API_BASE:"",WEB_ORIGIN:`http://localhost:${port}`,NUXT_PUBLIC_SITE_URL:`http://localhost:${port}`,CHROME_BIN:"chromium"};for(const [k,v] of Object.entries(env))console.log(`${k}=${JSON.stringify(v)}`)' "$web_port" \
    | docker exec -i "$resource_id" bash -c 'tee /etc/environment | sed "s/^/export /" > /etc/orca-workspace.env' >/dev/null
  project_root="$(value projectRoot ORCA_DOCKER_PROJECT_ROOT)"
  username="$(value username ORCA_DOCKER_USERNAME)"
  ssh -i "$key_file" -p "$ssh_port" -o BatchMode=yes -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=yes -o ConnectTimeout=10 "$username@127.0.0.1" \
    'test -d /home/mathis/projects/messeances/.git && opencode --version' >&2
  node -e 'const [id,name,port,key,root,user,web,api,hostKey]=process.argv.slice(1); console.log(JSON.stringify({schemaVersion:1,connection:{type:"ssh",projectRoot:root,target:{label:name,host:"127.0.0.1",port:Number(port),username:user,identityFile:key,identitiesOnly:true}},userData:{provider:"docker",resourceId:id,instance:name,sshPort:Number(port),hostKey,webUrl:`http://localhost:${web}`,apiUrl:`http://localhost:${api}`}}))' \
    "$resource_id" "$instance" "$ssh_port" "$key_file" "$project_root" "$username" "$web_port" "$api_port" "$host_key"
}
